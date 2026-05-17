// Package service provides Discord notification services.
//
// Author: Claude
package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type VocaloidRankingEntry struct {
	Name   string `json:"name"`
	Artist string `json:"artistString"`
	ID     int    `json:"id"`
	Url    string
}

type PvEntry struct {
	Service string `json:"service"`
	Url     string `json:"url"`
}

type PvServiceEntrySong struct {
	Pvs []PvEntry `json:"pvs"`
}

type PvServiceEntry struct {
	Song PvServiceEntrySong `json:"song"`
}

func getJSON(url string, target any) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// fetchPvYoutubeLinkById returns the best available PV link for a song,
// preferring YouTube > NicoNicoDouga > Bandcamp.
func fetchPvYoutubeLinkById(id string) (string, error) {
	var entry PvServiceEntry
	url := fmt.Sprintf("https://vocadb.net/api/songs/%s/with-rating", id)
	if err := getJSON(url, &entry); err != nil {
		return "", errors.Wrapf(err, "failed to get pvService for song %s", id)
	}

	var nicoUrl, youtubeUrl, bandcampUrl string
	for _, pv := range entry.Song.Pvs {
		switch pv.Service {
		case "Youtube":
			youtubeUrl = pv.Url
		case "NicoNicoDouga":
			nicoUrl = pv.Url
		case "Bandcamp":
			bandcampUrl = pv.Url
		}
	}

	switch {
	case youtubeUrl != "":
		return youtubeUrl, nil
	case nicoUrl != "":
		return nicoUrl, nil
	case bandcampUrl != "":
		return bandcampUrl, nil
	default:
		return "", fmt.Errorf("no supported PV url found for song %s", id)
	}
}

func fetchPvYouTubeLinks(ctx context.Context, entries []VocaloidRankingEntry) error {
	eg, ctx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(5)
	for idx, e := range entries {
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			url, _ := fetchPvYoutubeLinkById(strconv.Itoa(e.ID))
			entries[idx].Url = url
			return nil
		})
	}
	return eg.Wait()
}

func GetVocaloidRankingMessage() ([]string, error) {
	var entries []VocaloidRankingEntry
	url := "https://vocadb.net/api/songs/top-rated?" +
		"durationHours=24&filterBy=CreateDate" // TODO: support weekly and overall
	if err := getJSON(url, &entries); err != nil {
		return nil, errors.Wrap(err, "failed to fetch Vocaloid ranking data")
	}

	if err := fetchPvYouTubeLinks(context.Background(), entries); err != nil {
		return nil, err
	}

	var (
		buf      strings.Builder
		messages []string
	)
	for idx, entry := range entries {
		if entry.Url == "" {
			fmt.Fprintf(&buf, "%d. %s - %s\n", idx+1, entry.Name, entry.Artist)
		} else {
			fmt.Fprintf(&buf, "%d. [%s](<%s>) - %s\n", idx+1, entry.Name, entry.Url, entry.Artist)
		}
		if (idx+1)%10 == 0 {
			messages = append(messages, buf.String())
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		messages = append(messages, buf.String())
	}

	return messages, nil
}
