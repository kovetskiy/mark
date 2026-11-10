package manifest_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
)

// A space whose manifest page does not exist yet runs untracked, and the
// mapping is saved once the run has published that page.
func ExampleStore_SetManifestPageOptional() {
	ctx := context.Background()
	server := confluencetest.Start()
	defer server.Close()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	store := manifest.NewStore(api)
	store.SetManifestPage("Handbook")
	store.SetManifestPageOptional(true)

	if err := store.Record(ctx, "DOCS", "guide.md", "1", "Guide", ""); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("untracked:", store.UntrackedSpaces())

	// The run publishes the manifest page after the documents under it.
	if _, err := api.CreatePage(ctx, "DOCS", confluence.ContentPage, nil, "Handbook", ""); err != nil {
		fmt.Println(err)
		return
	}
	if err := store.Save(ctx); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("untracked after save:", store.UntrackedSpaces())

	// Output:
	// untracked: [DOCS]
	// untracked after save: []
}

// Without SetManifestPageOptional a missing manifest page is an error a caller
// can recognise.
func ExampleMissingPageError() {
	server := confluencetest.Start()
	defer server.Close()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	store := manifest.NewStore(confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}))
	store.SetManifestPage("Handbook")

	_, _, err := store.Lookup(context.Background(), "DOCS", "guide.md")

	var missing *manifest.MissingPageError
	if errors.As(err, &missing) {
		fmt.Printf("no page %q in space %s\n", missing.Page, missing.Space)
	}

	// Output:
	// no page "Handbook" in space DOCS
}
