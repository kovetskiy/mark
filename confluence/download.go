package confluence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxAttachmentDownload bounds what DownloadAttachment writes. It is well
// above what Confluence accepts as an upload by default (100 MB), so a real
// attachment is never cut short, while a server answering with an endless body
// cannot fill the disk.
const maxAttachmentDownload = 1 << 30

// ContentProperties returns every property stored against a page or blogpost:
// through v1 where it can, and through v2 on the scoped-token gateway, which
// refuses v1.
func (api *API) ContentProperties(contentID string) ([]Property, error) {
	if !api.gateway {
		return api.ListContentProperties(contentID)
	}

	collection, err := api.collectionOfV2(contentID)
	if err != nil {
		return nil, fmt.Errorf("unable to read properties of content %s: %w", contentID, err)
	}

	return api.listPropertiesV2(collection, contentID)
}

// AttachmentURL is where an attachment's content is downloaded from.
//
// Confluence names the download path relative to its context -- "/wiki" on
// Cloud, whatever the instance is mounted under on Server -- and a base URL
// configured the documented way already ends with that context. It is only
// added when it does not.
//
// A download link that names a host of its own is refused unless it is the
// configured one: the credentials go wherever the download does.
func (api *API) AttachmentURL(info AttachmentInfo) (string, error) {
	link := info.Links.Download
	if link == "" {
		return "", fmt.Errorf("attachment %q has no download link", info.Filename)
	}

	base, err := url.Parse(api.BaseURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse base URL %q: %w", api.BaseURL, err)
	}

	target, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("unable to parse download link %q of attachment %q: %w", link, info.Filename, err)
	}

	if target.IsAbs() {
		if !strings.EqualFold(target.Host, base.Host) {
			return "", fmt.Errorf(
				"attachment %q is served from %s rather than from %s; refusing to send credentials there",
				info.Filename, target.Host, base.Host,
			)
		}

		return target.String(), nil
	}

	prefix := strings.TrimSuffix(api.BaseURL, "/")
	if context := strings.TrimSuffix(info.Links.Context, "/"); context != "" && !strings.HasSuffix(prefix, context) {
		prefix += context
	}

	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}

	return prefix + link, nil
}

// DownloadAttachment writes the content of an attachment to w.
func (api *API) DownloadAttachment(ctx context.Context, info AttachmentInfo, w io.Writer) error {
	address, err := api.AttachmentURL(info)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("unable to build the download of attachment %q: %w", info.Filename, err)
	}

	api.authorize(request)

	response, err := api.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"unable to download attachment %q: %s answered %s",
			info.Filename, address, response.Status,
		)
	}

	written, err := io.Copy(w, io.LimitReader(response.Body, maxAttachmentDownload+1))
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}

	if written > maxAttachmentDownload {
		return fmt.Errorf("attachment %q is larger than %d bytes", info.Filename, maxAttachmentDownload)
	}

	return nil
}

// authorize puts the run's credentials on a request made outside gopencils,
// the same way a gopencils request carries them.
func (api *API) authorize(request *http.Request) {
	if api.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+api.bearerToken)
		return
	}

	if auth := api.rest.Api.BasicAuth; auth != nil {
		request.SetBasicAuth(auth.Username, auth.Password)
	}
}

// httpClient is the client every request of the run goes through, with its
// retries, timeouts and TLS settings.
func (api *API) httpClient() *http.Client {
	if client := api.rest.Api.Client; client != nil {
		return client
	}

	return http.DefaultClient
}
