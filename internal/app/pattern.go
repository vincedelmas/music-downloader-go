package app

import (
	"fmt"
	"regexp"
	"strings"
)

type namePattern struct{ expression *regexp.Regexp }

// Braces identify fields; a bracketed separator such as {-} is also accepted.
func compileNamePattern(pattern string) (namePattern, error) {
	var expression strings.Builder
	expression.WriteString(`^\s*`)
	counts := map[string]int{}
	for len(pattern) > 0 {
		if pattern[0] != '{' {
			i := strings.IndexByte(pattern, '{')
			if i < 0 {
				i = len(pattern)
			}

			literal := pattern[:i]
			if strings.Contains(literal, "}") {
				return namePattern{}, fmt.Errorf("close each opening brace in the name pattern")
			}

			expression.WriteString(regexp.QuoteMeta(literal))
			pattern = pattern[i:]
			continue
		}

		end := strings.IndexByte(pattern, '}')
		if end < 0 {
			return namePattern{}, fmt.Errorf("close each opening brace in the name pattern")
		}

		token := pattern[1:end]
		if strings.Contains(token, "{") {
			return namePattern{}, fmt.Errorf("use single braces, such as {artist}")
		}

		switch token {
		case "artist", "title":
			counts[token]++
			expression.WriteString("(?P<" + token + ">.+?)")
		case "skip":
			expression.WriteString(".+?")
		default:
			if token == "" {
				return namePattern{}, fmt.Errorf("empty braces in the name pattern")
			}
			expression.WriteString(regexp.QuoteMeta(token))
		}

		pattern = pattern[end+1:]
	}

	if counts["artist"] != 1 || counts["title"] != 1 {
		return namePattern{}, fmt.Errorf("include {artist} and {title} once each")
	}

	expression.WriteString(`\s*$`)
	re, err := regexp.Compile(expression.String())
	if err != nil {
		return namePattern{}, err
	}
	// Adjacent captures have no reliable boundary.
	if strings.Contains(expression.String(), ".+?)(?P<") {
		return namePattern{}, fmt.Errorf("put a separator between {artist} and {title}")
	}

	return namePattern{re}, nil
}

func (p namePattern) parse(name string) (string, string, error) {
	match := p.expression.FindStringSubmatch(name)
	if match == nil {
		return "", "", fmt.Errorf("“%s” doesn't match the name pattern", name)
	}

	artist := strings.TrimSpace(match[p.expression.SubexpIndex("artist")])
	title := strings.TrimSpace(match[p.expression.SubexpIndex("title")])
	if artist == "" || title == "" {
		return "", "", fmt.Errorf("the artist and title cannot be empty")
	}

	return artist, title, nil
}
