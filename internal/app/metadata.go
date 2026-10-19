package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	minimumRecordingScore    = 90
	rejectedReleaseScore     = -10_000
	maximumAlternativeCovers = 5
)

// Each service shares pacing and cooldowns across all download workers.
var musicBrainzRequests = metadataRequests{gate: make(chan struct{}, 1)}
var coverArtRequests = metadataRequests{gate: make(chan struct{}, 1)}

type metadataRequests struct {
	gate chan struct{}
	next time.Time
}

var metadataCache = struct {
	sync.Mutex
	items map[string]TrackMetadata
}{items: make(map[string]TrackMetadata)}

func (d *MusicDownloader) metadataRequest(ctx context.Context, address string, requests *metadataRequests) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MusicDownloader/"+version+" (https://github.com/"+releaseRepo+")")
	for attempt := 0; ; attempt++ {
		select {
		case requests.gate <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		timer := time.NewTimer(time.Until(requests.next))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			<-requests.gate
			return nil, ctx.Err()
		}
		requests.next = time.Now().Add(1100 * time.Millisecond)
		res, err := d.client.Do(req)
		if err != nil {
			<-requests.gate
			return nil, err
		}
		retry := res.StatusCode == 429 || res.StatusCode == 502 || res.StatusCode == 503 || res.StatusCode == 504
		if retry {
			delay := time.Duration(2<<attempt) * time.Second
			if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
				delay = max(delay, time.Duration(seconds)*time.Second)
			} else if until, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
				delay = max(delay, time.Until(until))
			}
			requests.next = time.Now().Add(delay)
		}
		<-requests.gate
		if !retry || attempt == 3 {
			return res, nil
		}
		res.Body.Close()
	}
}

func luceneQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func normalizedName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}

		return -1
	}, s)
}

func (d *MusicDownloader) metadata(ctx context.Context, t Track) (TrackMetadata, error) {
	key := normalizedName(t.Artist) + "/" + normalizedName(t.Title)
	metadataCache.Lock()
	cached, ok := metadataCache.items[key]
	metadataCache.Unlock()
	if ok {
		return cached, nil
	}

	query := url.Values{
		"query": {
			"recording:" + luceneQuote(t.Title) + " AND artist:" + luceneQuote(t.Artist) + " AND NOT video:true AND NOT comment:live",
		},
		"fmt":   {"json"},
		"limit": {"100"},
	}
	res, err := d.metadataRequest(ctx, "https://musicbrainz.org/ws/2/recording/?"+query.Encode(), &musicBrainzRequests)
	if err != nil {
		return TrackMetadata{}, err
	}

	defer res.Body.Close()
	if res.StatusCode != 200 {
		return TrackMetadata{}, fmt.Errorf("track details unavailable (%s)", res.Status)
	}

	var response struct {
		Recordings []musicBrainzRecording `json:"recordings"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&response); err != nil {
		return TrackMetadata{}, err
	}

	m, err := chooseRecording(response.Recordings, t)
	if err != nil {
		return TrackMetadata{}, err
	}

	metadataCache.Lock()
	metadataCache.items[key] = m
	metadataCache.Unlock()
	return m, nil
}

type artistCredit struct {
	Name   string `json:"name"`
	Join   string `json:"joinphrase"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}

type musicBrainzRecording struct {
	Title            string               `json:"title"`
	Score            int                  `json:"score"`
	Video            bool                 `json:"video"`
	Disambiguation   string               `json:"disambiguation"`
	FirstReleaseDate string               `json:"first-release-date"`
	Artists          []artistCredit       `json:"artist-credit"`
	Releases         []musicBrainzRelease `json:"releases"`
}

var alternateRecording = regexp.MustCompile(`(?i)\b(live|demo|remix|karaoke|re-recording|rerecording)\b`)

func artistName(credits []artistCredit, expected string) (string, bool) {
	var name strings.Builder
	matches := false
	for _, a := range credits {
		part := a.Name
		if part == "" {
			part = a.Artist.Name
		}

		name.WriteString(part)
		name.WriteString(a.Join)
		if normalizedName(part) == normalizedName(expected) || normalizedName(a.Artist.Name) == normalizedName(expected) {
			matches = true
		}
	}

	return name.String(), matches || normalizedName(name.String()) == normalizedName(expected)
}

func chooseRecording(recordings []musicBrainzRecording, t Track) (TrackMetadata, error) {
	var result TrackMetadata
	bestScore := rejectedReleaseScore
	bestDate := ""
	found := false
	for _, r := range recordings {
		titleMatches := normalizedName(r.Title) == normalizedName(t.Title)
		studioRecording := !r.Video && !alternateRecording.MatchString(r.Disambiguation)
		if r.Score < minimumRecordingScore || !titleMatches || !studioRecording {
			continue
		}

		name, matches := artistName(r.Artists, t.Artist)
		if !matches {
			continue
		}

		index := bestRelease(r.Releases, t.Artist)
		if index < 0 {
			continue
		}

		release := r.Releases[index]
		metadata, date := metadataForRelease(r, release, name, t.Artist)
		score := r.Score + releaseScore(release, t.Artist)

		if !found || score > bestScore || (score == bestScore && date != "" && (bestDate == "" || date < bestDate)) {
			result = metadata
			bestScore = score
			bestDate = date
			found = true
		}
	}

	if found {
		return result, nil
	}

	return TrackMetadata{}, fmt.Errorf("no matching track details found")
}

func validMBID(id string) bool {
	if len(id) != 36 {
		return false
	}

	for i, r := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}

	return true
}

type musicBrainzRelease struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Date    string         `json:"date"`
	Status  string         `json:"status"`
	Artists []artistCredit `json:"artist-credit"`
	Media   []struct {
		Format string `json:"format"`
	} `json:"media"`
	Group struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		FirstReleaseDate string   `json:"first-release-date"`
		Primary          string   `json:"primary-type"`
		Secondary        []string `json:"secondary-types"`
	} `json:"release-group"`
}

func releaseScore(r musicBrainzRelease, artist string) int {
	if r.Status != "" && r.Status != "Official" {
		return rejectedReleaseScore
	}

	videoMedia := 0
	for _, m := range r.Media {
		format := strings.ToLower(m.Format)
		if strings.Contains(format, "video") || strings.Contains(format, "blu-ray") || format == "vhs" || format == "vcd" || format == "laserdisc" {
			videoMedia++
		}
	}

	if len(r.Media) > 0 && videoMedia == len(r.Media) {
		return rejectedReleaseScore
	}

	score := 0
	mediaScore := 0
	for _, m := range r.Media {
		switch m.Format {
		case "CD", "Enhanced CD", "SACD":
			mediaScore = max(mediaScore, 10)
		case "Digital Media", "12\" Vinyl", "Vinyl":
			mediaScore = max(mediaScore, 6)
		}
	}

	score += mediaScore
	if r.Status == "Official" {
		score += 20
	}

	switch r.Group.Primary {
	case "Album":
		score += 100
	case "Single":
		score += 60
	case "EP":
		score += 50
	default:
		score += 10
	}

	_, ownArtist := artistName(r.Artists, artist)
	if ownArtist {
		score += 40
	}

	for _, kind := range r.Group.Secondary {
		switch kind {
		case "Live", "Remix", "Demo", "DJ-mix":
			return rejectedReleaseScore
		case "Compilation":
			if ownArtist {
				score -= 25
			} else {
				score -= 140
			}
		}
	}

	return score
}

func bestRelease(releases []musicBrainzRelease, artist string) int {
	best := -1
	bestScore := rejectedReleaseScore
	for i, r := range releases {
		score := releaseScore(r, artist)
		if score <= rejectedReleaseScore {
			continue
		}

		if best < 0 || score > bestScore || (score == bestScore && releaseDateEarlier(r.Date, releases[best].Date)) {
			best = i
			bestScore = score
		}
	}

	return best
}

func releaseDateEarlier(a, b string) bool {
	if a == "" {
		return false
	}

	if b == "" {
		return true
	}
	// A year-only date is not evidence of an earlier edition within that same year.
	if len(a) >= 4 && len(b) >= 4 && a[:4] == b[:4] && ((len(a) == 4) != (len(b) == 4)) {
		return len(a) > len(b)
	}

	return a < b
}

// metadataForRelease combines recording details with the chosen album edition.
func metadataForRelease(recording musicBrainzRecording, release musicBrainzRelease, artistName, artistQuery string) (TrackMetadata, string) {
	metadata := TrackMetadata{
		Title:  recording.Title,
		Artist: artistName,
		Album:  release.Title,
	}

	if release.Group.Title != "" {
		metadata.Album = release.Group.Title
	}

	date := recording.FirstReleaseDate
	if release.Date != "" {
		date = release.Date
	}

	if release.Group.FirstReleaseDate != "" {
		date = release.Group.FirstReleaseDate
	}

	if len(date) >= 4 {
		metadata.Year = date[:4]
	}

	if validMBID(release.ID) {
		metadata.CoverURL = "https://coverartarchive.org/release/" + release.ID + "/front-1200"
	}

	metadata.CoverAlternatives = alternativeCovers(recording.Releases, release, artistQuery)
	if validMBID(release.Group.ID) {
		metadata.CoverFallbackURL = "https://coverartarchive.org/release-group/" + release.Group.ID + "/front-1200"
	}

	return metadata, date
}

// alternativeCovers keeps fallbacks within the same album, ordered like release selection.
func alternativeCovers(releases []musicBrainzRelease, selected musicBrainzRelease, artist string) []string {
	alternatives := make([]musicBrainzRelease, 0, len(releases))
	for _, candidate := range releases {
		sameAlbum := selected.Group.ID != "" && candidate.Group.ID == selected.Group.ID
		if selected.Group.ID == "" {
			sameAlbum = normalizedName(candidate.Title) == normalizedName(selected.Title)
		}

		if !sameAlbum {
			continue
		}

		eligible := releaseScore(candidate, artist) > rejectedReleaseScore && validMBID(candidate.ID) && candidate.ID != selected.ID
		if eligible {
			alternatives = append(alternatives, candidate)
		}
	}

	sort.SliceStable(alternatives, func(i, j int) bool {
		firstScore := releaseScore(alternatives[i], artist)
		secondScore := releaseScore(alternatives[j], artist)
		if firstScore != secondScore {
			return firstScore > secondScore
		}

		return releaseDateEarlier(alternatives[i].Date, alternatives[j].Date)
	})

	var urls []string
	for i, release := range alternatives {
		if i >= maximumAlternativeCovers {
			break
		}

		urls = append(urls, "https://coverartarchive.org/release/"+release.ID+"/front-1200")
	}

	return urls
}
