package confluence

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/kovetskiy/gopencils"
)

// The operations a page can be restricted on.
const (
	RestrictRead   = "read"
	RestrictUpdate = "update"
)

var restrictedOperations = []string{RestrictRead, RestrictUpdate}

// accountIDPrefix names a Cloud user by accountId rather than by full name,
// for when the full name is ambiguous.
const accountIDPrefix = "accountid:"

// PageRestriction is who a document says may perform an operation on its
// page, by name. One that names nobody lifts the operation's restrictions.
type PageRestriction struct {
	Operation string
	Users     []string
	Groups    []string
}

func (r PageRestriction) restricts() bool {
	return len(r.Users) > 0 || len(r.Groups) > 0
}

// Subjects are who may perform an operation, as Confluence names them: users
// by accountId on Cloud and by username on Server and Data Center, groups by
// name. No subjects at all leaves the operation unrestricted.
type Subjects struct {
	Users  []string
	Groups []string
}

func (s Subjects) restricts() bool {
	return len(s.Users) > 0 || len(s.Groups) > 0
}

// Restrictions are the subjects of each operation. An operation that is not
// there is unrestricted.
type Restrictions map[string]Subjects

// ResolvedRestrictions are a document's restrictions with every user resolved,
// and the authenticated user added to each operation that names anyone, so
// that publishing cannot lock mark out of its own page. Resolving only reads,
// so a dry run does it too, and a name that resolves to nobody fails before a
// real run creates anything.
type ResolvedRestrictions struct {
	declared Restrictions
	// self is how Confluence names the authenticated user, or "" when no
	// operation names anyone and nobody had to ask.
	self  string
	cloud bool
}

// ResolveRestrictions resolves the users declared restrictions name.
//
// On Cloud a user is named by full name, which has to match exactly one user,
// or as accountid:<id>; the authenticated user may also be named by its login
// or email. On Server and Data Center a user is named by username.
func (api *API) ResolveRestrictions(declared []PageRestriction) (*ResolvedRestrictions, error) {
	cloud, err := api.Cloud()
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedRestrictions{declared: Restrictions{}, cloud: cloud}

	var current *User
	if slices.ContainsFunc(declared, PageRestriction.restricts) {
		current, err = api.GetCurrentUser()
		if err != nil {
			return nil, fmt.Errorf("unable to resolve the authenticated user; refusing restrictions that could lock it out: %w", err)
		}
		resolved.self = current.Username
		if cloud {
			resolved.self = current.AccountID
		}
		if resolved.self == "" {
			return nil, fmt.Errorf("the authenticated user has no accountId or username to restrict a page to")
		}
	}

	for _, restriction := range declared {
		if !slices.Contains(restrictedOperations, restriction.Operation) {
			return nil, fmt.Errorf("unknown restriction operation %q", restriction.Operation)
		}

		subjects := resolved.declared[restriction.Operation]
		for _, name := range restriction.Users {
			user := name
			if cloud {
				user, err = api.restrictedAccountID(name, current)
				if err != nil {
					return nil, err
				}
			}
			subjects.Users = append(subjects.Users, user)
		}
		subjects.Groups = append(subjects.Groups, restriction.Groups...)
		if subjects.restricts() {
			// First, so that its own spelling is the one kept.
			subjects.Users = append([]string{resolved.self}, subjects.Users...)
		}
		resolved.declared[restriction.Operation] = resolved.normalise(subjects)
	}

	return resolved, nil
}

// restrictedAccountID is the accountId of the Cloud user name names.
//
// The user search matches any full name that contains the one it is given, so
// it is only a candidate list here: access control cannot take the first of
// two Jane Does, or a Jane Doe-Smith, for the Jane Doe that was meant.
func (api *API) restrictedAccountID(name string, current *User) (string, error) {
	if len(name) >= len(accountIDPrefix) && strings.EqualFold(name[:len(accountIDPrefix)], accountIDPrefix) {
		// The id keeps its own case; only the prefix is matched without it.
		id := strings.TrimSpace(name[len(accountIDPrefix):])
		if id == "" {
			return "", fmt.Errorf("restricted user %q names no accountId", name)
		}
		return id, nil
	}

	if current.isNamed(name) || (api.username != "" && strings.EqualFold(name, api.username)) {
		return current.AccountID, nil
	}

	candidates, err := api.searchUsers(name)
	if err != nil {
		return "", fmt.Errorf("unable to resolve restricted user %q: %w", name, err)
	}

	var matches []User
	for _, candidate := range candidates {
		if candidate.isNamed(name) && candidate.AccountID != "" &&
			!slices.ContainsFunc(matches, func(u User) bool { return u.AccountID == candidate.AccountID }) {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0].AccountID, nil
	case 0:
		return "", fmt.Errorf("unable to resolve restricted user %q: no user has exactly that name: %w", name, errNoSuchUser)
	default:
		ids := make([]string, len(matches))
		for i, match := range matches {
			ids[i] = accountIDPrefix + match.AccountID
		}
		return "", fmt.Errorf(
			"restricted user %q is ambiguous: %d users have that name; name the one meant as one of %s",
			name, len(matches), strings.Join(ids, ", "),
		)
	}
}

// normalise sorts subjects and names each once: Confluence refuses a
// duplicate, and two sets that say the same thing have to compare equal.
// Usernames and group names match without regard to case; an accountId is an
// identifier.
func (r *ResolvedRestrictions) normalise(s Subjects) Subjects {
	return Subjects{
		Users:  uniqueSorted(s.Users, !r.cloud),
		Groups: uniqueSorted(s.Groups, true),
	}
}

func uniqueSorted(names []string, foldCase bool) []string {
	key := func(name string) string {
		if foldCase {
			return strings.ToLower(name)
		}
		return name
	}

	seen := map[string]bool{}
	out := []string{}
	for _, name := range names {
		if name == "" || seen[key(name)] {
			continue
		}
		seen[key(name)] = true
		out = append(out, name)
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(key(a), key(b)) })

	return out
}

// RestrictionPlan is how a page goes from the restrictions it has to the ones
// its document declares, in two steps around the content update.
//
// Interim holds before the content is written: for each operation, whichever
// of the current and final restrictions allows fewer people. Tightening
// therefore happens before new content can be seen by anyone it is meant to
// be kept from, and loosening only once the content that justified the old
// restriction has been replaced -- and a run that fails in between leaves the
// page no more open than it was or than it is meant to be.
type RestrictionPlan struct {
	pageID                  string
	current, interim, final Restrictions
}

// Changes reports whether the plan changes the page's restrictions at all.
func (p *RestrictionPlan) Changes() bool {
	return !p.current.equal(p.final)
}

// PlanRestrictions plans page's restrictions. An operation the document does
// not name keeps what it has, however it was set. lockUpdates is --edit-lock:
// the authenticated user is kept among those who may update, so the replacing
// PUT never lifts the edit lock even for a moment. created says the page has
// just been made, and so has nothing to read back.
func (api *API) PlanRestrictions(page *PageInfo, resolved *ResolvedRestrictions, lockUpdates, created bool) (*RestrictionPlan, error) {
	current := Restrictions{}
	if !created {
		var err error
		current, err = api.GetPageRestrictions(page.ID)
		if err != nil {
			return nil, err
		}
	}

	final := Restrictions{}
	for _, operation := range restrictedOperations {
		subjects, declared := resolved.declared[operation]
		if !declared {
			subjects = current[operation]
		}
		if operation == RestrictUpdate && lockUpdates && resolved.self != "" {
			subjects.Users = append(slices.Clone(subjects.Users), resolved.self)
		}
		final[operation] = resolved.normalise(subjects)
	}

	interim := Restrictions{}
	for _, operation := range restrictedOperations {
		interim[operation] = resolved.narrower(current[operation], final[operation])
	}
	// Confluence refuses a user who may update but not read. Current and
	// final are each as consistent as whoever set them made them, and only a
	// group's members know whether a user is in it, so an interim that takes
	// its read from one of them keeps that one's updaters. Updaters taken
	// from the other are kept only if the interim read names them.
	if read, update := interim[RestrictRead], interim[RestrictUpdate]; read.restricts() && update.restricts() {
		var kept Subjects
		for _, side := range []Restrictions{current, final} {
			if read.equal(side[RestrictRead]) {
				kept.Users = append(kept.Users, intersect(update, side[RestrictUpdate], resolved).Users...)
				kept.Groups = append(kept.Groups, intersect(update, side[RestrictUpdate], resolved).Groups...)
			}
		}
		named := intersect(update, read, resolved)
		kept.Users = append(kept.Users, named.Users...)
		kept.Groups = append(kept.Groups, named.Groups...)
		interim[RestrictUpdate] = resolved.withSelf(kept)
	}

	return &RestrictionPlan{pageID: page.ID, current: current, interim: interim, final: final}, nil
}

// narrower is whichever of a and b allows fewer people: an unrestricted
// operation allows anyone, and of two restricted ones only those both name.
func (r *ResolvedRestrictions) narrower(a, b Subjects) Subjects {
	switch {
	case !a.restricts():
		return r.normalise(b)
	case !b.restricts():
		return r.normalise(a)
	default:
		return r.withSelf(intersect(a, b, r))
	}
}

// withSelf adds the authenticated user to a restricted operation, which an
// intersection can otherwise leave mark out of halfway through its own run.
func (r *ResolvedRestrictions) withSelf(s Subjects) Subjects {
	if r.self != "" {
		s.Users = append([]string{r.self}, s.Users...)
	}
	return r.normalise(s)
}

func intersect(a, b Subjects, r *ResolvedRestrictions) Subjects {
	a, b = r.normalise(a), r.normalise(b)
	in := func(names []string, foldCase bool) func(string) bool {
		return func(name string) bool {
			return slices.ContainsFunc(names, func(other string) bool {
				return other == name || foldCase && strings.EqualFold(other, name)
			})
		}
	}

	var out Subjects
	for _, user := range a.Users {
		if in(b.Users, !r.cloud)(user) {
			out.Users = append(out.Users, user)
		}
	}
	for _, group := range a.Groups {
		if in(b.Groups, true)(group) {
			out.Groups = append(out.Groups, group)
		}
	}

	return out
}

func (r Restrictions) equal(other Restrictions) bool {
	for _, operation := range restrictedOperations {
		if !r[operation].equal(other[operation]) {
			return false
		}
	}
	return true
}

// equal compares two normalised subjects.
func (s Subjects) equal(other Subjects) bool {
	return slices.EqualFunc(s.Users, other.Users, strings.EqualFold) &&
		slices.EqualFunc(s.Groups, other.Groups, strings.EqualFold)
}

// ApplyInterim puts the plan's interim restrictions on the page, if they are
// not what it has already. It goes before the content update.
func (api *API) ApplyInterim(plan *RestrictionPlan) error {
	if plan.current.equal(plan.interim) {
		return nil
	}
	return api.putPageRestrictions(plan.pageID, plan.interim)
}

// ApplyFinal puts the plan's final restrictions on the page, if the interim
// ones are not them already. It goes after the content update.
func (api *API) ApplyFinal(plan *RestrictionPlan) error {
	if plan.interim.equal(plan.final) {
		return nil
	}
	return api.putPageRestrictions(plan.pageID, plan.final)
}

// GetPageRestrictions reads who may read and update the page.
func (api *API) GetPageRestrictions(pageID string) (Restrictions, error) {
	// A list that fills its page, or says it is longer than what came back,
	// may have been cut short.
	type subjectList[T any] struct {
		Results   []T `json:"results"`
		Limit     int `json:"limit"`
		TotalSize int `json:"totalSize"`
	}
	partial := func(results, limit, total int) bool {
		return total > results || limit > 0 && results >= limit
	}
	type operation struct {
		Restrictions struct {
			User  subjectList[User] `json:"user"`
			Group subjectList[struct {
				Name string `json:"name"`
			}] `json:"group"`
		} `json:"restrictions"`
	}
	var response map[string]operation

	var expand []string
	for _, op := range restrictedOperations {
		expand = append(expand, op+".restrictions.user", op+".restrictions.group")
	}

	request, err := api.v1().
		Res("content").
		Id(pageID).
		Res("restriction").
		Res("byOperation", &response).
		Get(map[string]string{"expand": strings.Join(expand, ",")})
	if err != nil {
		return nil, newTransportError(request, "read the restrictions of page "+pageID, err)
	}
	if err := api.restrictionStatusError(request); err != nil {
		return nil, err
	}

	cloud := api.IsCloud()
	restrictions := Restrictions{}
	for _, name := range restrictedOperations {
		op := response[name].Restrictions
		// Comparing against part of a list could lift a restriction nobody
		// asked to lift.
		if partial(len(op.User.Results), op.User.Limit, op.User.TotalSize) ||
			partial(len(op.Group.Results), op.Group.Limit, op.Group.TotalSize) {
			return nil, fmt.Errorf("page %s has more %s restrictions than one request returns", pageID, name)
		}

		var subjects Subjects
		for _, user := range op.User.Results {
			id := user.Username
			if cloud {
				id = user.AccountID
			}
			subjects.Users = append(subjects.Users, id)
		}
		for _, group := range op.Group.Results {
			subjects.Groups = append(subjects.Groups, group.Name)
		}
		restrictions[name] = Subjects{
			Users:  uniqueSorted(subjects.Users, !cloud),
			Groups: uniqueSorted(subjects.Groups, true),
		}
	}

	return restrictions, nil
}

// putPageRestrictions replaces every read and update restriction on the page
// with restrictions. Both operations are always sent, so that the request
// says what each one ends up as.
func (api *API) putPageRestrictions(pageID string, restrictions Restrictions) error {
	cloud := api.IsCloud()

	payload := make([]map[string]any, 0, len(restrictedOperations))
	for _, operation := range restrictedOperations {
		subjects := restrictions[operation]

		users := []map[string]any{}
		for _, id := range subjects.Users {
			if cloud {
				users = append(users, map[string]any{"type": "known", "accountId": id})
			} else {
				users = append(users, map[string]any{"type": "known", "username": id})
			}
		}
		groups := []map[string]any{}
		for _, name := range subjects.Groups {
			groups = append(groups, map[string]any{"type": "group", "name": name})
		}

		payload = append(payload, map[string]any{
			"operation":    operation,
			"restrictions": map[string]any{"user": users, "group": groups},
		})
	}

	var result any
	request, err := api.v1().
		Res("content").
		Id(pageID).
		Res("restriction", &result).
		Put(payload)
	if err != nil {
		return newTransportError(request, "set the restrictions of page "+pageID, err)
	}

	return api.restrictionStatusError(request)
}

// restrictionStatusError is the error a restriction request's status means,
// or nil for success. Server and Data Center before 8.8 have no restriction
// endpoints to speak of, which reads as a missing page or method.
func (api *API) restrictionStatusError(request *gopencils.Resource) error {
	status := request.Raw.StatusCode
	if status == http.StatusOK || status == http.StatusNoContent {
		return nil
	}
	if !api.IsCloud() && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		return fmt.Errorf("confluence server/datacenter version is too old to support page restrictions via REST API (requires Confluence 8.8.0 or newer; status: %d)", status)
	}
	return newErrorStatusNotOK(request)
}
