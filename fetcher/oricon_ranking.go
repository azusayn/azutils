package fetcher

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
	"github.com/pkg/errors"
)

const (
	DomainOricon  = "www.oricon.co.jp"
	OriconRankUrl = "https://www.oricon.co.jp/rank/"
)

type OriconRankingTrend string

const (
	OriconRankingTrendUp        = "up"
	OriconRankingTrendNew       = "new"
	OriconRankingTrendDown      = "down"
	OriconRankingTrendStay      = "stay"
	OriconRankingTrendUnknowned = "unknowned"
)

func getOriconRaningTrendFromStr(str string) OriconRankingTrend {
	switch str {
	case "status new":
		return OriconRankingTrendNew
	case "status up":
		return OriconRankingTrendUp
	case "status down":
		return OriconRankingTrendDown
	case "status stay":
		return OriconRankingTrendStay
	default:
		return OriconRankingTrendUnknowned
	}
}

func oriconRankingTrendToEmoji(trend OriconRankingTrend) string {
	switch trend {
	case OriconRankingTrendNew:
		return "🆕"
	case OriconRankingTrendUp:
		return "🔼"
	case OriconRankingTrendDown:
		return "🔻"
	case OriconRankingTrendStay:
		return "▶️"
	default:
		return ""
	}
}

type OriconRankingDataEntry struct {
	Title  string
	Artist string
	Link   string
	Trend  OriconRankingTrend
}

type OriconRankingData struct {
	Rule    string
	Entries []OriconRankingDataEntry
}

type OriconRankingDataArray []OriconRankingData

func FetchRankingDataFromOricon() (OriconRankingDataArray, error) {
	const RankDataSelector = "#content-main > div.content-main-inner > div.content-rank-main > div > article > section:nth-child(2)"
	var retErr error
	c := colly.NewCollector(
		colly.AllowedDomains(DomainOricon),
	)

	getRankDataByRule := func(index int, rankData *OriconRankingData, e *colly.HTMLElement) {
		prefix := fmt.Sprintf("div:nth-child(%d) > ", index)

		// rule.
		e.DOM.Find(prefix + "h3").Each(func(i int, s *goquery.Selection) {
			rankData.Rule = s.Text()
		})

		// entries.
		e.DOM.Find(prefix + "div > div").Each(func(i int, s *goquery.Selection) {
			s.Find("dl").Each(func(i int, dl *goquery.Selection) {
				var title, artist, href string
				var trend OriconRankingTrend = OriconRankingTrendUnknowned
				dl.Find("a").Each(func(_ int, a *goquery.Selection) {
					href, _ = a.Attr("href")
				})
				dl.Find("h4").Each(func(i int, h4 *goquery.Selection) {
					title = h4.Text()
				})
				dl.Find("p").Each(func(i int, p *goquery.Selection) {
					if p.HasClass("name") {
						artist = p.Text()
						return
					}
					if class, ok := p.Attr("class"); ok {
						trend = getOriconRaningTrendFromStr(class)
					}
				})
				rankData.Entries = append(rankData.Entries, OriconRankingDataEntry{
					Title:  title,
					Artist: artist,
					Link:   href,
					Trend:  trend,
				})
			})
		})
	}

	dailySingleRankData := OriconRankingData{}
	dailyAlbumRankData := OriconRankingData{}
	weeklySingleRankData := OriconRankingData{}
	weeklyAlbumRankData := OriconRankingData{}

	c.OnHTML(RankDataSelector, func(e *colly.HTMLElement) {
		getRankDataByRule(2, &dailySingleRankData, e)
		getRankDataByRule(6, &dailyAlbumRankData, e)
		getRankDataByRule(4, &weeklySingleRankData, e)
		getRankDataByRule(7, &weeklyAlbumRankData, e)
	})

	c.OnError(func(r *colly.Response, err error) {
		retErr = err
	})

	c.Visit(OriconRankUrl)

	return []OriconRankingData{
		dailySingleRankData,
		dailyAlbumRankData,
		weeklySingleRankData,
		weeklyAlbumRankData,
	}, retErr
}

func (oriconRankData OriconRankingDataArray) Dump() string {
	var oriconRank strings.Builder
	for _, data := range oriconRankData {
		oriconRank.WriteString(fmt.Sprintf("### %s\n", data.Rule))
		for _, entry := range data.Entries {
			if entry.Link == "" {
				oriconRank.WriteString(fmt.Sprintf("%s - %s ", entry.Title, entry.Artist))
			} else {
				oriconRank.WriteString(fmt.Sprintf("[%s](<https://%s/%s>) - %s ", entry.Title, DomainOricon, entry.Link, entry.Artist))
			}
			oriconRank.WriteString(oriconRankingTrendToEmoji(entry.Trend))
			oriconRank.WriteRune('\n')
		}
		oriconRank.WriteRune('\n')
	}
	return oriconRank.String()
}

func GetOriconRankingDataMessage() (string, error) {
	rankData, err := FetchRankingDataFromOricon()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get ranking data from Oricon")
	}
	return rankData.Dump(), nil
}
