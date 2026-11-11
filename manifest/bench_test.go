package manifest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
)

// BenchmarkResolveRenamedFor is a run of a few thousand documents new to a
// scope that already tracks as many, each asking whether it is a rename, as
// an unrecorded document does twice with RenameSimilarity. One op is the
// whole run's questions.
func BenchmarkResolveRenamedFor(b *testing.B) {
	const documents = 3000

	ctx := context.Background()
	server := confluencetest.Start()
	b.Cleanup(server.Close)
	server.AddSpace("DOCS")

	old := make([]string, documents)
	for i := range old {
		old[i] = fmt.Sprintf("old/%d.md", i)
	}
	first := storeFor(server, "", "gen", nil, old)
	for i, key := range old {
		if err := first.Record(ctx, "DOCS", key, fmt.Sprint(i+1), key, fmt.Sprintf("hash-%d", i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := first.Save(ctx); err != nil {
		b.Fatal(err)
	}

	renamed := make([]string, documents)
	for i := range renamed {
		renamed[i] = fmt.Sprintf("new/%d.md", i)
	}

	for b.Loop() {
		b.StopTimer()
		next := storeFor(server, "", "gen", nil, renamed)
		if _, _, _, err := next.ResolveRenamedFor(ctx, "DOCS", renamed[0], "load"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		for i, key := range renamed {
			if _, _, _, err := next.ResolveRenamedFor(ctx, "DOCS", key, fmt.Sprintf("other-%d", i)); err != nil {
				b.Fatal(err)
			}
		}
	}
}
