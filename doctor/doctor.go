// Package doctor checks that mark can reach a Confluence, log in to it, and
// publish to the spaces it is pointed at, and says what to change when it
// cannot.
package doctor

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
)

// Status is how a check came out.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is one line of the report.
type Check struct {
	Status Status
	Name   string
	Detail string
	// Hint says what to change, for a check that did not pass.
	Hint string
}

// permission is an operation publishing needs in a space, and what needs it.
type permission struct {
	operation, targetType string
	// required permissions fail the check when missing; the others warn, since
	// only some runs need them.
	required bool
	what     string
}

var permissions = []permission{
	{"create", "page", true, "create pages"},
	{"create", "attachment", true, "upload attachments"},
	{"create", "blogpost", false, "create blog posts (Type: blogpost)"},
	{"delete", "page", false, "delete pages (--on-orphan delete)"},
	{"archive", "page", false, "archive pages (--on-orphan archive)"},
	{"restrict_content", "", false, "restrict pages (--edit-lock, Restriction headers)"},
}

// Run checks the instance api talks to, and each space in spaces, and writes
// a line per check to out. It returns an error when any check failed.
func Run(api *confluence.API, baseURL, username string, spaces []string, out io.Writer) error {
	checks := run(api, baseURL, username, spaces)

	failed := 0
	for _, check := range checks {
		_, _ = fmt.Fprintf(out, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
		if check.Hint != "" {
			_, _ = fmt.Fprintf(out, "       %s\n", check.Hint)
		}
		if check.Status == Fail {
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	return nil
}

func run(api *confluence.API, baseURL, username string, spaces []string) []Check {
	host := ""
	if u, err := url.Parse(baseURL); err == nil {
		host = strings.ToLower(u.Hostname())
	}

	user, err := api.GetCurrentUser()
	if err != nil {
		return []Check{loginFailure(err, host, username)}
	}

	checks := []Check{{Status: OK, Name: "login", Detail: describeUser(user)}}

	cloud, err := api.Cloud()
	if err != nil {
		checks = append(checks, Check{Status: Warn, Name: "platform", Detail: err.Error()})
	} else {
		checks = append(checks, Check{Status: OK, Name: "platform", Detail: describePlatform(cloud, host)})
	}

	if len(spaces) == 0 {
		checks = append(checks, Check{
			Status: Warn, Name: "space", Detail: "no space to check",
			Hint: "name one as an argument, or with --space or the configuration file",
		})
	}

	for _, space := range spaces {
		checks = append(checks, checkSpace(api, space)...)
	}

	return checks
}

// loginFailure explains a failed first request, which is where a wrong URL,
// a certificate, a proxy and wrong credentials all show up.
func loginFailure(err error, host, username string) Check {
	check := Check{Status: Fail, Name: "login", Detail: err.Error()}

	var dns *net.DNSError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	cloud := host == "api.atlassian.com" || strings.HasSuffix(host, ".atlassian.net") || strings.HasSuffix(host, ".jira.com")

	switch {
	case errors.As(err, &dns):
		check.Hint = "the host in --base-url does not resolve; check it for typos"
	case errors.As(err, &unknownAuthority), errors.As(err, &hostname):
		check.Hint = "the server's TLS certificate is not trusted; add its CA to the system's " +
			"trust store (or SSL_CERT_FILE), or use --insecure-skip-tls-verify for a test instance"
	case errors.Is(err, confluence.ErrNotFound):
		check.Hint = "--base-url does not lead to Confluence's REST API; on Cloud it ends in /wiki, " +
			"and on Server or Data Center it includes any context path, such as /confluence"
	case errors.Is(err, confluence.ErrUnauthorized) && host == "api.atlassian.com":
		check.Hint = "through the API gateway, --username is your Atlassian account email and " +
			"--password a scoped API token with read:confluence-user among its scopes"
	case errors.Is(err, confluence.ErrUnauthorized) && cloud && username == "":
		check.Hint = "on Cloud, --username is your Atlassian account email and --password an API token; " +
			"a token without a username is how Server and Data Center take a personal access token"
	case errors.Is(err, confluence.ErrUnauthorized) && cloud:
		check.Hint = "on Cloud, --username is your Atlassian account email (not a display name) and " +
			"--password an API token from id.atlassian.com, not the account's password"
	case errors.Is(err, confluence.ErrUnauthorized):
		check.Hint = "on Server and Data Center, a personal access token goes in --password with no " +
			"--username; a username takes that user's password"
	}

	return check
}

func describeUser(user *confluence.User) string {
	name := user.DisplayName
	if name == "" {
		name = user.PublicName
	}
	var ids []string
	for _, id := range []string{user.Email, user.Username, user.AccountID} {
		if id != "" && id != name {
			ids = append(ids, id)
		}
	}
	switch {
	case name == "" && len(ids) == 0:
		return "logged in"
	case name == "":
		return "logged in as " + strings.Join(ids, ", ")
	case len(ids) == 0:
		return "logged in as " + name
	default:
		return fmt.Sprintf("logged in as %s (%s)", name, strings.Join(ids, ", "))
	}
}

func describePlatform(cloud bool, host string) string {
	switch {
	case host == "api.atlassian.com":
		return "Confluence Cloud, through the API gateway"
	case cloud:
		return "Confluence Cloud"
	default:
		return "Confluence Server or Data Center"
	}
}

func checkSpace(api *confluence.API, key string) []Check {
	name := "space " + key

	title, operations, err := api.SpaceOperations(key)
	switch {
	case errors.Is(err, confluence.ErrNotFound):
		return []Check{{
			Status: Fail, Name: name, Detail: "not found",
			Hint: "the key is wrong, or the account cannot see the space; keys are case-sensitive",
		}}
	case err != nil && !errors.Is(err, confluence.ErrNoOperations):
		return []Check{{Status: Fail, Name: name, Detail: err.Error()}}
	}

	detail := "readable"
	if title != "" && title != key {
		detail = fmt.Sprintf("%q is readable", title)
	}
	checks := []Check{{Status: OK, Name: name, Detail: detail}}

	// Publishing only compares pages against the home page, so a space without
	// one is published to all the same.
	home, homeErr := api.FindHomePage(key)
	switch {
	case errors.Is(homeErr, confluence.ErrNoHomePage):
		checks = append(checks, Check{
			Status: Warn, Name: name, Detail: "no home page",
			Hint: "a page without a Parent header is published at the top of the space",
		})
	case homeErr != nil:
		checks = append(checks, Check{
			Status: Fail, Name: name, Detail: "unable to find the home page: " + homeErr.Error(),
		})
	default:
		checks = append(checks, Check{
			Status: OK, Name: name, Detail: fmt.Sprintf("home page %q (%s)", home.Title, home.ID),
		})
	}

	if errors.Is(err, confluence.ErrNoOperations) {
		return append(checks, Check{
			Status: Warn, Name: name, Detail: err.Error() + ", so its permissions are unchecked",
		})
	}

	var can, cannot []string
	missingRequired := false
	for _, p := range permissions {
		if permitted(operations, p) {
			can = append(can, p.what)
			continue
		}
		cannot = append(cannot, p.what)
		missingRequired = missingRequired || p.required
	}

	if len(can) > 0 {
		checks = append(checks, Check{Status: OK, Name: name, Detail: "can " + strings.Join(can, ", ")})
	}
	if len(cannot) > 0 {
		status := Warn
		if missingRequired {
			status = Fail
		}
		checks = append(checks, Check{
			Status: status, Name: name, Detail: "cannot " + strings.Join(cannot, ", "),
			Hint: "a space administrator grants these under the space's permissions",
		})
	}

	return checks
}

func permitted(operations []confluence.Operation, p permission) bool {
	for _, op := range operations {
		if op.Operation == p.operation && (p.targetType == "" || op.TargetType == p.targetType) {
			return true
		}
	}
	return false
}
