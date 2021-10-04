package mark

import (
	"bytes"
	"fmt"

	"github.com/reconquest/pkg/log"
	"github.com/adrg/frontmatter"
)

func FrontMatter(data []byte) (*Meta, []byte, error) {
	var matter struct {
		Space       string   `yaml:"space"`
		Parents     []string `yaml:"parents"`
		Type        string   `yaml:"type"`
		Title       string   `yaml:"title"`
		Sidebar     string   `yaml:"sidebar`
		Layout      string   `yaml:"layout"`
		Attachments []string `yaml:"attachments"`
		Tags   []string `yaml:"tags"`
		Labels []string `yaml:"labels"`
	}

	rest, err := frontmatter.Parse(bytes.NewReader(data), &matter)
	if err != nil {
		log.Info(nil, "error")
		// Treat error.
	}
	// NOTE: If a front matter must be present in the input data, use
	//       frontmatter.MustParse instead.

	fmt.Printf("%+v\n", matter)

	var (
		meta *Meta
	)

	if meta == nil {
		meta = &Meta{}
		meta.Type = "page" //Default if not specified
		meta.Attachments = make(map[string]string)
	}

	meta.Parents = matter.Parents
	meta.Space = matter.Space
	meta.Layout = matter.Layout
	meta.Sidebar = matter.Sidebar
	meta.Labels = matter.Labels

	if matter.Type != "" {
		meta.Type = matter.Type
	}

	meta.Attachments = make(map[string]string)
	elements := matter.Attachments
	for i := 0; i < len(elements); i += 2 {
		meta.Attachments[elements[i]] = elements[i]
	}

	return meta, rest, nil
}
