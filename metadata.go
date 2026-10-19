package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MusicBrainz allows one request per second. Share pacing across all download workers.
var musicBrainzGate = make(chan struct{}, 1)
var musicBrainzLast time.Time
var metadataCache = struct {
	sync.Mutex
	items map[string]TrackMetadata
}{items: make(map[string]TrackMetadata)}

func waitForMusicBrainz(ctx context.Context) error {
	select {
	case musicBrainzGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	delay := time.Until(musicBrainzLast.Add(1100 * time.Millisecond))
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			<-musicBrainzGate
			return ctx.Err()
		}
	}
	musicBrainzLast = time.Now()
	<-musicBrainzGate
	return nil
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
	if err := waitForMusicBrainz(ctx); err != nil {
		return TrackMetadata{}, err
	}
	query := url.Values{"query": {"recording:" + luceneQuote(t.Title) + " AND artist:" + luceneQuote(t.Artist) + " AND NOT video:true AND NOT comment:live"}, "fmt": {"json"}, "limit": {"100"}}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://musicbrainz.org/ws/2/recording/?"+query.Encode(), nil)
	if err != nil {
		return TrackMetadata{}, err
	}
	req.Header.Set("User-Agent", "MusicDownloader/"+version+" (https://github.com/"+releaseRepo+")")
	req.Header.Set("Accept", "application/json")
	res, err := d.client.Do(req)
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
	bestScore := -10000
	bestDate := ""
	found := false
	for _, r := range recordings {
		if r.Score < 90 || normalizedName(r.Title) != normalizedName(t.Title) || r.Video || alternateRecording.MatchString(r.Disambiguation) {
			continue
		}
		name, matches := artistName(r.Artists, t.Artist)
		if !matches {
			continue
		}
		m := TrackMetadata{Title: r.Title, Artist: name}
		date := r.FirstReleaseDate
		score := r.Score
		index := bestRelease(r.Releases, t.Artist)
		if index < 0 {
			continue
		}
		if index >= 0 {
			rel := r.Releases[index]
			score += releaseScore(rel, t.Artist)
			m.Album = rel.Title
			if rel.Group.Title != "" {
				m.Album = rel.Group.Title
			}
			if rel.Date != "" {
				date = rel.Date
			}
			if rel.Group.FirstReleaseDate != "" {
				date = rel.Group.FirstReleaseDate
			}
			if validMBID(rel.ID) {
				m.CoverURL = "https://coverartarchive.org/release/" + rel.ID + "/front-1200"
			}
			alternatives := make([]musicBrainzRelease, 0, len(r.Releases))
			for _, other := range r.Releases {
				sameAlbum := rel.Group.ID != "" && other.Group.ID == rel.Group.ID
				if rel.Group.ID == "" {
					sameAlbum = normalizedName(other.Title) == normalizedName(rel.Title)
				}
				if sameAlbum && releaseScore(other, t.Artist) > -10000 && validMBID(other.ID) && other.ID != rel.ID {
					alternatives = append(alternatives, other)
				}
			}
			sort.SliceStable(alternatives, func(i, j int) bool {
				a, b := releaseScore(alternatives[i], t.Artist), releaseScore(alternatives[j], t.Artist)
				if a != b {
					return a > b
				}
				return releaseDateEarlier(alternatives[i].Date, alternatives[j].Date)
			})
			for i, other := range alternatives {
				if i >= 5 {
					break
				}
				m.CoverAlternatives = append(m.CoverAlternatives, "https://coverartarchive.org/release/"+other.ID+"/front-1200")
			}
			if validMBID(rel.Group.ID) {
				m.CoverFallbackURL = "https://coverartarchive.org/release-group/" + rel.Group.ID + "/front-1200"
			}
		}
		if len(date) >= 4 {
			m.Year = date[:4]
		}
		if !found || score > bestScore || (score == bestScore && date != "" && (bestDate == "" || date < bestDate)) {
			result = m
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
		return -10000
	}
	videoMedia := 0
	for _, m := range r.Media {
		format := strings.ToLower(m.Format)
		if strings.Contains(format, "video") || strings.Contains(format, "blu-ray") || format == "vhs" || format == "vcd" || format == "laserdisc" {
			videoMedia++
		}
	}
	if len(r.Media) > 0 && videoMedia == len(r.Media) {
		return -10000
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
			return -10000
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
	bestScore := -10000
	for i, r := range releases {
		score := releaseScore(r, artist)
		if score <= -10000 {
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
