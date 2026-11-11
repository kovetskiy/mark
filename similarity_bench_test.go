package mark

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
)

// BenchmarkRunTexts is a scoped run of a few thousand documents spread over
// several spaces, each with keys gone from the scope, asking for the texts
// the similarity renames compare once per space, as the first document of
// each space does. Every document is recorded, so nothing is asked of
// Confluence but the manifests. One op is one run's questions.
func BenchmarkRunTexts(b *testing.B) {
	const (
		documents = 3000
		spaces    = 5
	)

	ctx := context.Background()
	server := confluencetest.Start()
	b.Cleanup(server.Close)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	body := strings.Repeat("A decision about how the service stores its records, and why. ", 40)
	docs := make([]Document, documents)
	keys := make([]string, documents)
	for i := range docs {
		space := fmt.Sprintf("S%d", i%spaces)
		keys[i] = fmt.Sprintf("adr/%d.md", i)
		docs[i] = Document{
			Path:    keys[i],
			Content: fmt.Appendf(nil, "<!-- Space: %s -->\n<!-- Title: Decision %d -->\n\n%s%d\n", space, i, body, i),
		}
	}

	seed := manifest.NewStore(api)
	seed.SetRunFiles("", nil)
	seed.SetRunDocuments("adr", keys)
	for i := range spaces {
		server.AddSpace(fmt.Sprintf("S%d", i))
	}
	for i, key := range keys {
		if err := seed.Record(ctx, fmt.Sprintf("S%d", i%spaces), key, fmt.Sprint(i+1), key, "h"); err != nil {
			b.Fatal(err)
		}
	}
	if err := seed.Save(ctx); err != nil {
		b.Fatal(err)
	}

	inputs, err := indexInputs(nil, docs)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		b.StopTimer()
		tracker := manifest.NewStore(api)
		tracker.SetRunFiles("", nil)
		tracker.SetRunDocuments("adr", keys)
		run := &publishRun{api: api, tracker: tracker, inputs: inputs}
		run.titles = &runTitles{inputs: inputs.all}
		b.StartTimer()

		for i := range spaces {
			if _, _, err := run.runTexts(ctx, fmt.Sprintf("S%d", i)); err != nil {
				b.Fatal(err)
			}
		}
	}
}
