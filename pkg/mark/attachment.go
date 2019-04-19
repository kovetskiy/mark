package mark

import "github.com/kovetskiy/mark/pkg/confluence"

type Attachment struct {
	Name string
}

func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	base string,
	names []string,
) {
	err := api.GetAttachments(page.ID)
	if err != nil {
		panic(err)
	}
}
