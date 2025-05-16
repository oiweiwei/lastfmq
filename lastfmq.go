package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

var (
	bandName                           string
	refFormat                          string
	tags, similarArtists, wiki, events bool
	pageNum                            int
	pageOffset                         int
	workersNum                         int
)

var defaultClient = &http.Client{
	// CheckRedirect: func(req *http.Request, via []*http.Request) error {
	//	return http.ErrUseLastResponse
	// },
	Timeout: 60 * time.Second,
}

func init() {
	flag.StringVar(&bandName, "band", "", "band name (for convenience)")
	flag.BoolVar(&tags, "tags", false, "read artists tags")
	flag.BoolVar(&similarArtists, "similar-artists", false, "read similar artists")
	flag.BoolVar(&wiki, "wiki", false, "read wiki")
	flag.StringVar(&refFormat, "wiki-ref-format", `%q`, "the reference format for the wiki references in text")
	flag.BoolVar(&events, "events", false, "read events")
	flag.IntVar(&pageNum, "similar-artists-pages", 5, "number of pages for similar artists")
	flag.IntVar(&pageOffset, "similar-artists-pages-offset", 0, "page offset for similar artists")
	flag.IntVar(&workersNum, "workers", 1, "the number of workers")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "lastfmq - read last.fm band information")
		fmt.Fprintln(flag.CommandLine.Output(), "usage: lastfmq [flags] <band_name>")
		flag.PrintDefaults()
	}

	flag.Parse()

	if bandName == "" {
		bandName = strings.Join(flag.Args(), " ")
	}
}

const (
	tagsURL               = "https://www.last.fm/music/%s/+tags"
	similarArtistsPageURL = "https://www.last.fm/music/%s/+similar?page=%d"
	wikiURL               = "https://www.last.fm/music/%s/+wiki"
	overviewURL           = "https://www.last.fm/music/%s"
	eventsURL             = "https://www.last.fm/music/%s/+events"
)

type bandDesc struct {
	BandName       string   `json:"band_name,omitempty"`
	Scrobbles      int      `json:"scrobbles,omitempty"`
	Listeners      int      `json:"listeners,omitempty"`
	YearsActive    string   `json:"years_active,omitempty"`
	FoundedIn      string   `json:"founded_in,omitempty"`
	Born           string   `json:"born,omitempty"`
	BornIn         string   `json:"born_in,omitempty"`
	Wiki           *Wiki    `json:"wiki,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	SimilarArtists []string `json:"similar_artists,omitempty"`
	Years          []string `json:"events_years,omitempty"`
}

func main() {

	if bandName == "" {
		fmt.Fprintln(os.Stderr, "band name is required")
		flag.Usage()
		os.Exit(1)
	}

	var (
		err      error
		bandDesc *bandDesc
	)

	if bandDesc, err = readOverview(context.TODO(), bandName); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if wiki {
		if bandDesc.Wiki, err = readWiki(context.TODO(), bandName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if tags {
		if bandDesc.Tags, bandDesc.SimilarArtists, err = readTags(bandName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if similarArtists {

		readSimilarArtists := readSimilarArtists
		if workersNum > 1 {
			readSimilarArtists = readSimilarArtistsAsync
		}

		if bandDesc.SimilarArtists, err = readSimilarArtists(bandName, pageNum, pageOffset); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if events {
		if bandDesc.Years, err = readEventYears(context.TODO(), bandName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if err = json.NewEncoder(os.Stdout).Encode(bandDesc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}

const (
	pageSize = 10
)

func readSimilarArtistsAsync(bandName string, pages, offset int) ([]string, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type outValue struct {
		page    int
		artists []string
	}

	pageCount, outC, errC, wg := new(atomic.Int32), make(chan outValue), make(chan error, 1), new(sync.WaitGroup)
	defer close(outC)

	for i := 0; i < workersNum; i++ {

		wg.Add(1)

		go func(ctx context.Context) {

			defer wg.Done()

			for pageNum := int(pageCount.Add(1)); pageNum <= pages+offset; pageNum = int(pageCount.Add(1)) {

				similar, err := readSimilarArtistsPage(ctx, bandName, pageNum)
				if err != nil {
					errC <- err
					continue
				}

				if len(similar) == 0 {
					return
				}

				outC <- outValue{pageNum, similar}
			}

		}(ctx)
	}

	doneC := make(chan struct{})

	go func() {
		wg.Wait()
		doneC <- struct{}{}
	}()

	var errs []error

	var ret = make([]string, pageSize*(pages))
	var retSize int

loop:
	for {
		select {
		case <-doneC:
			break loop // all goroutines terminated.
		case err := <-errC:
			errs = append(errs, err) // error occurred, wait for other goroutines.
		case val := <-outC:
			retSize += len(val.artists)
			copy(ret[(val.page-1)*pageSize:val.page*pageSize], val.artists)
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("read_similar_artists: %v", errs[0])
	}

	return ret[:retSize], nil
}

func readSimilarArtists(bandName string, pages, offset int) ([]string, error) {

	ret := []string{}

	for i := 1 + offset; i <= pages+offset; i++ {
		similar, err := readSimilarArtistsPage(context.TODO(), bandName, i)
		if err != nil {
			return nil, fmt.Errorf("read_similar_artists: %v", err)
		}

		ret = append(ret, similar...)
	}

	return ret, nil
}

func readOverview(ctx context.Context, bandName string) (*bandDesc, error) {

	if bandName == "" {
		return nil, fmt.Errorf("read_overview: band name is required")
	}

	ret := &bandDesc{}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(overviewURL, bandName), nil)
	if err != nil {
		return nil, fmt.Errorf("read_overview: new_request_with_context", err)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read_overview: http_get: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("read_overview: band not found: %s", bandName)
		}
		return nil, fmt.Errorf("read_overview: status: %s (%+v)", resp.Status, resp.Header)
	}

	var (
		startMetadata bool
		dt            string
		intAbbr       string
	)

	tokenizer := html.NewTokenizer(resp.Body)

	for tok := tokenizer.Next(); tokenizer.Err() == nil; tok = tokenizer.Next() {

		switch tok {
		case html.EndTagToken:
			if startMetadata {
				if containsAttr(tokenizer, TagAttr("dl", "")) != "" {
					startMetadata = false
				}
			}
		case html.StartTagToken:
			if startMetadata {

				switch containsAttr(tokenizer,
					TagAttr("dt", ""),
					TagAttr("dd", "")) {

				case "dt":

					if tokenizer.Next() != html.TextToken {
						continue
					}
					dt = string(tokenizer.Text())

				case "dd":

					if tokenizer.Next() != html.TextToken {
						continue
					}

					switch dt {
					case "Years Active":
						ret.YearsActive = string(tokenizer.Text())
					case "Founded In":
						ret.FoundedIn = string(tokenizer.Text())
					case "Born":
						ret.Born = string(tokenizer.Text())
					case "Born In":
						ret.BornIn = string(tokenizer.Text())
					}
				}
			} else {
				switch attr := containsAttr(tokenizer,
					TagAttr("dl", "class", "catalogue-metadata"),
					TagAttr("h1", "class", "header-new-title"),
					TagAttr("abbr", "title", "*"),
					TagAttr("h4", "class", "header-metadata-tnew-title")); attr {
				case "catalogue-metadata":
					startMetadata = true
				case "header-new-title":
					if tokenizer.Next() != html.TextToken {
						continue
					}
					ret.BandName = string(tokenizer.Text())
				case "header-metadata-tnew-title":
					if tokenizer.Next() != html.TextToken {
						continue
					}
					intAbbr = strings.TrimSpace(string(tokenizer.Text()))
				case "":
					// noop.
				default:
					// abbr title=*
					switch intAbbr {
					case "Scrobbles":
						ret.Scrobbles, _ = strconv.Atoi(strings.ReplaceAll(attr, ",", ""))
					case "Listeners":
						ret.Listeners, _ = strconv.Atoi(strings.ReplaceAll(attr, ",", ""))
					default:
					}

				}
			}
		}
	}

	if err := tokenizer.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read_overview: tokenizer: %v", err)
	}

	return ret, nil

}

func readEventYears(ctx context.Context, bandName string) ([]string, error) {

	if bandName == "" {
		return nil, fmt.Errorf("read_event_years: band name is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(eventsURL, bandName), nil)
	if err != nil {
		return nil, fmt.Errorf("read_event_years: new_request_with_context", err)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read_event_years: http_get: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read_event_years: status: %s (%+v)", resp.Status, resp.Header)
	}

	tokenizer := html.NewTokenizer(resp.Body)

	var startNav bool
	var years []string

loop:
	for tok := tokenizer.Next(); tokenizer.Err() == nil; tok = tokenizer.Next() {

		switch tok {
		case html.EndTagToken:
			if startNav {
				if containsAttr(tokenizer, TagAttr("nav", "")) != "" {
					break loop
				}
			}
		case html.StartTagToken:
			if startNav {
				if containsAttr(tokenizer, TagAttr("a", "class", "secondary-nav-item-link")) != "" {
					if tokenizer.Next() != html.TextToken {
						continue
					}
					txt := strings.TrimSpace(string(tokenizer.Text()))
					if txt == "" {
						continue
					}
					years = append(years, txt)
				}
			} else {
				if containsAttr(tokenizer, TagAttr("nav", "aria-label", "Event Year Navigation")) != "" {
					startNav = true
				}
			}
		}
	}

	if err := tokenizer.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read_event_years: tokenizer: %v", err)
	}

	return years, nil
}

type Event struct {
	Date    string
	Address *EventAddress
	Lineup  string
}

type EventAddress struct {
	Name       string
	Street     string
	Locality   string
	Code       string
	Country    string
	Telephone  string
	DetailsWeb string
	MapWeb     string
}

type Wiki struct {
	Members []*Member `json:"members"`
	Bio     []string  `json:"bio"`
	Refs    []*Ref    `json:"refs"`
}

type Ref struct {
	Name      string `json:"name"`
	Reference string `json:"reference"`
}

type Member struct {
	Name        string `json:"name"`
	YearsActive string `json:"years_active"`
}

func readWiki(ctx context.Context, bandName string) (*Wiki, error) {

	if bandName == "" {
		return nil, fmt.Errorf("read_wiki: band name is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(wikiURL, bandName), nil)
	if err != nil {
		return nil, fmt.Errorf("read_wiki: new_request_with_context", err)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read_wiki: http_get: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read_wiki: status: %s (%+v)", resp.Status, resp.Header)
	}

	var (
		wiki      = new(Wiki)
		txt       string
		startWiki bool
	)

	tokenizer := html.NewTokenizer(resp.Body)

	for tok := tokenizer.Next(); tokenizer.Err() == nil; tok = tokenizer.Next() {

		switch tok {
		case html.EndTagToken:
			if startWiki {
				if containsAttr(tokenizer, TagAttr("ul", "")) != "" {
					startWiki = false
				}
			}
		case html.StartTagToken:
			switch containsAttr(tokenizer,
				TagAttr("ul", "class", "factbox"),
				TagAttr("div", "class", "wiki-content"),
				TagAttr("h4", "class", "factbox-heading")) {

			case "factbox":

				startWiki = true

			case "factbox-heading":

				if !startWiki {
					continue
				}

				if tokenizer.Next() != html.TextToken {
					continue
				}

				title := string(tokenizer.Text())
				if title != "Members" {
					continue
				}

				for next := tokenizer.Next(); tokenizer.Err() == nil; next = tokenizer.Next() {

					if next == html.EndTagToken && containsAttr(tokenizer, TagAttr("ul", "")) != "" {
						break
					}

					if next != html.TextToken {
						continue
					}

					if txt = strings.TrimSpace(string(tokenizer.Text())); txt == "" {
						continue
					}

					if strings.HasPrefix(txt, "(") && len(wiki.Members) > 0 {
						wiki.Members[len(wiki.Members)-1].YearsActive = txt
					} else {
						wiki.Members = append(wiki.Members, &Member{Name: txt})
					}
				}

			case "wiki-content":

				var (
					bio       []string
					refsSeen  = make(map[string]string)
					quote, br bool
					txt, ref  string
				)

			readbio_loop:
				for next := tokenizer.Next(); tokenizer.Err() == nil; next = tokenizer.Next() {

					switch containsAttr(tokenizer,
						TagAttr("p", ""),
						TagAttr("div", ""),
						TagAttr("br", ""),
						TagAttr("a", "")) {

					case "div":

						if next == html.EndTagToken {
							break readbio_loop
						}

					case "p":

						if next != html.EndTagToken {
							continue
						}
						if len(bio) > 0 {
							wiki.Bio, bio = append(wiki.Bio, strings.Split(strings.TrimSpace(strings.Join(bio, "")), "\n")...), nil
						}

						continue

					case "br":

						br = true

					case "a":

						if next != html.StartTagToken {
							break
						}

						quote = true

						// we didn't read attributes, so can setup and iterator.
						for iter := NewIter(tokenizer); iter.Next(); {
							if key, val := iter.Attrs(); key == "href" {
								ref = val
								break
							}
						}
					}

					if next != html.TextToken {
						continue
					}

					if txt = string(tokenizer.Text()); txt == "" {
						continue
					}

					if ref != "" {
						if _, seen := refsSeen[txt]; !seen {
							wiki.Refs, refsSeen[txt] = append(wiki.Refs, &Ref{Name: txt, Reference: ref}), ref
						}
					}

					if br && len(bio) > 0 {
						bio[len(bio)-1] += "\n"
					}

					if quote {
						txt = fmt.Sprintf(refFormat, txt)
					}

					bio, br, quote, ref = append(bio, txt), false, false, ""
				}
			}
		}
	}

	if err := tokenizer.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read_wiki: tokenizer: %v", err)
	}

	return wiki, nil
}

func readSimilarArtistsPage(ctx context.Context, bandName string, pageNum int) ([]string, error) {

	if bandName == "" {
		return nil, fmt.Errorf("read_similar_artists: page %d: band name is required", pageNum)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(similarArtistsPageURL, bandName, pageNum), nil)
	if err != nil {
		return nil, fmt.Errorf("read_similar_artists: page %d: new_request_with_context: %v", pageNum, err)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read_similar_artists: page %d: http_get: %v", pageNum, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read_similar_artists: status: %s (%+v)", resp.Status, resp.Header)
	}

	// check page number in case of overflow.
	if resp.Request.URL.Query().Get("page") != strconv.Itoa(pageNum) {
		return nil, nil
	}

	tokenizer := html.NewTokenizer(resp.Body)

	var (
		similar      []string
		startSimilar bool
	)

	for tok := tokenizer.Next(); tokenizer.Err() == nil; tok = tokenizer.Next() {

		switch tok {
		case html.EndTagToken:
			if startSimilar {
				if containsAttr(tokenizer, TagAttr("ol", "")) != "" {
					if startSimilar {
						startSimilar = false
					}
				}
			}
		case html.StartTagToken:
			if startSimilar {
				if containsAttr(tokenizer, TagAttr("a", "class", "link-block-target")) != "" {
					if tokenizer.Next() != html.TextToken {
						continue
					}
					if startSimilar {
						similar = append(similar, string(tokenizer.Text()))
					}
				}
			} else {
				if containsAttr(tokenizer, TagAttr("ol", "class", "similar-artists")) != "" {
					startSimilar = true
				}
			}
		}
	}

	if tokenizer.Err() != io.EOF {
		return nil, fmt.Errorf("read_similar_artists: page %d: tokenizer: %v", pageNum, err)
	}

	return similar, nil
}

func readTags(bandName string) ([]string, []string, error) {

	if bandName == "" {
		return nil, nil, fmt.Errorf("read_tags: band name is required")
	}

	resp, err := http.Get(fmt.Sprintf(tagsURL, bandName))
	if err != nil {
		return nil, nil, fmt.Errorf("read_tags: http_get: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("read_tags: status: %s", resp.Status)
	}

	tokenizer := html.NewTokenizer(resp.Body)

	var (
		tags, similar = []string{}, []string{}
		startTags     bool
		startSimilar  bool
	)

	numEntites := 3

loop:
	for tok := tokenizer.Next(); tokenizer.Err() == nil; tok = tokenizer.Next() {

		switch tok {
		case html.EndTagToken:
			if startTags || startSimilar {
				if containsAttr(tokenizer, TagAttr("ol", "")) != "" {
					if startTags {
						numEntites--
						startTags = false
					}
					if startSimilar {
						numEntites--
						startSimilar = false
					}
				}

				if numEntites == 0 {
					break loop
				}
			}
		case html.StartTagToken:
			if startTags || startSimilar {
				if containsAttr(tokenizer, TagAttr("a", "class", "link-block-target")) != "" {
					if tokenizer.Next() != html.TextToken {
						continue
					}
					if startTags {
						tags = append(tags, string(tokenizer.Text()))
					}

					if startSimilar {
						similar = append(similar, string(tokenizer.Text()))
					}
				}
			} else {
				switch containsAttr(tokenizer,
					TagAttr("ol", "class", "big-tags", "similar-items-sidebar")) {
				case "big-tags":
					startTags = true
				case "similar-items-sidebar":
					startSimilar = true
				}
			}
		}
	}

	if err := tokenizer.Err(); err != nil && err != io.EOF {
		return nil, nil, fmt.Errorf("read_tags: tokenizer: %v", err)
	}

	return tags, similar, nil
}

type tagAttr struct {
	tagName  string
	attrName string
	attrVals []string
}

func TagAttr(tagName, attrName string, attrVals ...string) *tagAttr {
	return &tagAttr{tagName, attrName, attrVals}
}

// containsAttr function will return matched attribute value or token name (if attribute value is omitted).
func containsAttr(tokenizer *html.Tokenizer, tagAttrs ...*tagAttr) string {

	tagName, hasAttr := tokenizer.TagName()
	iter := NewIter(tokenizer)

	for _, tagAttr := range tagAttrs {
		if tagAttr.tagName != string(tagName) {
			continue
		}

		if tagAttr.attrName == "" {
			return tagAttr.tagName
		}

		if !hasAttr {
			return ""
		}

		iter.Reset()

		for iter.Next() {
			key, val := iter.Attrs()
			if key != tagAttr.attrName {
				continue
			}
			if len(tagAttr.attrVals) == 0 {
				return tagAttr.attrName
			}
			if tagAttr.attrVals[0] == "*" {
				return val
			}
			for _, attrVal := range tagAttr.attrVals {
				if strings.Contains(val, attrVal) {
					return attrVal
				}
			}
		}
	}
	return ""
}

type iterTagAttr struct {
	*html.Tokenizer
	pos  int
	keys []string
	vals []string
}

func NewIter(tokenizer *html.Tokenizer) *iterTagAttr {
	return &iterTagAttr{Tokenizer: tokenizer, pos: -1}
}

func (i *iterTagAttr) Next() bool {
	if i.pos++; i.Tokenizer != nil {
		key, val, more := i.TagAttr()
		if !more {
			i.Tokenizer = nil
		}
		i.keys, i.vals = append(i.keys, string(key)), append(i.vals, string(val))
	}
	return i.pos < len(i.keys)
}

func (i *iterTagAttr) Reset() {
	i.pos = -1
}

func (i *iterTagAttr) Attrs() (string, string) {
	return i.keys[i.pos], i.vals[i.pos]
}
