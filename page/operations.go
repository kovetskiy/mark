package page

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
)

// Ref names a page for the page commands: by ID, or by Title within Space.
type Ref struct {
	ID    string
	Space string
	Title string
}

func (ref Ref) String() string {
	if ref.ID != "" {
		return ref.ID
	}
	return fmt.Sprintf("%q in space %s", ref.Title, ref.Space)
}

// Resolve reads the page a Ref names, with its space, ancestors and version,
// and whatever else expand asks for.
func Resolve(api *confluence.API, ref Ref, expand ...string) (*confluence.PageInfo, error) {
	id := ref.ID

	if id == "" {
		if ref.Space == "" || ref.Title == "" {
			return nil, errors.New("a page is named by its id, or by its space and title")
		}

		var found *confluence.PageInfo
		for _, pageType := range []string{"page", "blogpost"} {
			var err error
			found, err = api.FindPage(ref.Space, ref.Title, pageType)
			if err != nil {
				return nil, fmt.Errorf("unable to find page %s: %w", ref, err)
			}
			if found != nil {
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("no page or blog post titled %q in space %s", ref.Title, ref.Space)
		}

		id = found.ID
	}

	page, err := api.GetPageByIDExpanded(id, strings.Join(append([]string{"ancestors", "version", "space"}, expand...), ","))
	if err != nil {
		return nil, fmt.Errorf("unable to read page %s: %w", id, err)
	}
	// Every operation compares or looks things up by the space.
	if page.Space.Key == "" {
		return nil, fmt.Errorf("unable to tell which space page %s is in", id)
	}

	return page, nil
}

// MoveOptions says where Move puts a page: under Parent, or next to a sibling
// with Before or After. A sibling and a Parent together require the sibling
// to be the Parent's child.
type MoveOptions struct {
	Parent *Ref
	Before *Ref
	After  *Ref
	DryRun bool
}

// Move moves a page within its space, and its descendants with it.
func Move(api *confluence.API, ref Ref, options MoveOptions, out io.Writer) error {
	target, err := Resolve(api, ref)
	if err != nil {
		return err
	}
	if target.Type == "blogpost" {
		return fmt.Errorf("%s is a blog post, and blog posts have no place in the page tree", describe(target))
	}

	if options.Before != nil || options.After != nil {
		return moveBeside(api, target, options, out)
	}
	if options.Parent == nil {
		return errors.New("say where to move the page: under a parent, or before or after a sibling")
	}

	parent, err := Resolve(api, *options.Parent)
	if err != nil {
		if options.Parent.ID == "" {
			return err
		}
		// A folder is not content v1 can read; the same id may still name one.
		folder, folderErr := api.GetFolderByID(options.Parent.ID)
		if folderErr != nil {
			return err
		}
		return moveUnderFolder(api, target, folder, options.DryRun, out)
	}

	if err := checkMove(target, parent); err != nil {
		return err
	}
	if parentOf(target) == parent.ID {
		fmt.Fprintf(out, "%s is already under %s\n", describe(target), describe(parent))
		return nil
	}

	if options.DryRun {
		fmt.Fprintf(out, "would move %s under %s\n", describe(target), describe(parent))
		return nil
	}
	if err := api.MoveContentAppend(target.ID, parent.ID); err != nil {
		return fmt.Errorf("unable to move %s under %s: %w", describe(target), describe(parent), err)
	}
	fmt.Fprintf(out, "moved %s under %s\n", describe(target), describe(parent))

	return nil
}

func moveBeside(api *confluence.API, target *confluence.PageInfo, options MoveOptions, out io.Writer) error {
	position, siblingRef := "before", options.Before
	if options.After != nil {
		position, siblingRef = "after", options.After
	}

	sibling, err := Resolve(api, *siblingRef)
	if err != nil {
		return err
	}
	if sibling.ID == target.ID {
		return fmt.Errorf("cannot place %s %s itself", describe(target), position)
	}
	if err := checkMove(target, sibling); err != nil {
		return err
	}
	if len(sibling.Ancestors) == 0 {
		// Confluence documents that placing content beside a top-level page
		// can move it to the root of the space instead.
		return fmt.Errorf("%s is at the top of its space, and mark places pages only beside pages that have a parent", describe(sibling))
	}

	if options.Parent != nil {
		parent, err := Resolve(api, *options.Parent)
		if err != nil {
			return err
		}
		if parentOf(sibling) != parent.ID {
			return fmt.Errorf("%s is not a child of %s", describe(sibling), describe(parent))
		}
	}

	if options.DryRun {
		fmt.Fprintf(out, "would place %s %s %s\n", describe(target), position, describe(sibling))
		return nil
	}

	if position == "before" {
		err = api.MoveContentBefore(target.ID, sibling.ID)
	} else {
		err = api.MoveContentAfter(target.ID, sibling.ID)
	}
	if err != nil {
		return fmt.Errorf("unable to place %s %s %s: %w", describe(target), position, describe(sibling), err)
	}
	fmt.Fprintf(out, "placed %s %s %s\n", describe(target), position, describe(sibling))

	return nil
}

func moveUnderFolder(api *confluence.API, target *confluence.PageInfo, folder *confluence.FolderInfo, dryRun bool, out io.Writer) error {
	spaceID, err := api.GetSpaceID(target.Space.Key)
	if err != nil {
		return fmt.Errorf("unable to read space %s: %w", target.Space.Key, err)
	}
	if folder.SpaceID != "" && folder.SpaceID != spaceID {
		return fmt.Errorf("folder %q (%s) is in another space, and mark moves pages only within their space", folder.Title, folder.ID)
	}

	if dryRun {
		fmt.Fprintf(out, "would move %s into folder %q (%s)\n", describe(target), folder.Title, folder.ID)
		return nil
	}
	if err := api.MoveContentAppend(target.ID, folder.ID); err != nil {
		return fmt.Errorf("unable to move %s into folder %q (%s): %w", describe(target), folder.Title, folder.ID, err)
	}
	fmt.Fprintf(out, "moved %s into folder %q (%s)\n", describe(target), folder.Title, folder.ID)

	return nil
}

// checkMove refuses to put target next to or under destination when that
// would leave the space or make the page its own ancestor.
func checkMove(target, destination *confluence.PageInfo) error {
	if destination.Space.Key != target.Space.Key {
		return fmt.Errorf("%s is in space %s and %s in space %s: mark moves pages only within their space",
			describe(target), target.Space.Key, describe(destination), destination.Space.Key)
	}
	if destination.ID == target.ID || isAncestor(target.ID, destination) {
		return fmt.Errorf("cannot move %s beneath itself", describe(target))
	}
	return nil
}

// Rename changes a page's title and leaves its content as it is. The new
// version is a minor edit, so watchers are not notified.
func Rename(api *confluence.API, ref Ref, title string, dryRun bool, out io.Writer) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("a page needs a title")
	}

	target, err := Resolve(api, ref, "body.storage")
	if err != nil {
		return err
	}
	if target.Title == title {
		fmt.Fprintf(out, "%s already has that title\n", describe(target))
		return nil
	}

	existing, err := api.FindPage(target.Space.Key, title, target.Type)
	if err != nil {
		return fmt.Errorf("unable to look for a page titled %q in space %s: %w", title, target.Space.Key, err)
	}
	if existing != nil && existing.ID != target.ID {
		return fmt.Errorf("space %s already has a %s titled %q (%s)", target.Space.Key, target.Type, title, existing.ID)
	}

	old, oldTitle := describe(target), target.Title
	if dryRun {
		fmt.Fprintf(out, "would rename %s to %q\n", old, title)
		return nil
	}

	target.Title = title
	err = api.UpdatePage(target, target.Body.Storage.Value, true, fmt.Sprintf("renamed from %q", oldTitle), "", "")
	if err != nil {
		return fmt.Errorf("unable to rename %s: %w", old, err)
	}
	fmt.Fprintf(out, "renamed %s to %q\n", old, title)

	return nil
}

// Archive archives a page without children. Confluence Cloud only, and
// Confluence archives after it answers, so this reports the request.
func Archive(api *confluence.API, ref Ref, dryRun bool, out io.Writer) error {
	target, err := Resolve(api, ref)
	if err != nil {
		return err
	}
	if err := checkRemovable(api, target); err != nil {
		return err
	}

	children, err := directChildren(api, target, removeFolders)
	if err != nil {
		return err
	}
	if len(children) > 0 {
		return fmt.Errorf("%s has %d child page(s): archive or move them first", describe(target), len(children))
	}

	if dryRun {
		fmt.Fprintf(out, "would archive %s\n", describe(target))
		return nil
	}
	if err := api.ArchivePage(target.ID); err != nil {
		return fmt.Errorf("unable to archive %s: %w", describe(target), err)
	}
	fmt.Fprintf(out, "asked Confluence to archive %s\n", describe(target))

	return nil
}

// removeFolders is why removing a page is refused when a folder is beneath it.
const removeFolders = "mark does not remove folders: move it first"

// Delete moves a page to the trash, where Confluence keeps it recoverable. A
// page with children is refused unless recursive is set, and then its
// descendants are trashed first, deepest first, so that every delete is of a
// page with nothing beneath it.
func Delete(api *confluence.API, ref Ref, recursive, dryRun bool, out io.Writer) error {
	target, err := Resolve(api, ref)
	if err != nil {
		return err
	}
	if err := checkRemovable(api, target); err != nil {
		return err
	}

	children, err := directChildren(api, target, removeFolders)
	if err != nil {
		return err
	}
	if len(children) > 0 && !recursive {
		return fmt.Errorf("%s has %d child page(s): pass --recursive to trash them too, or move them first",
			describe(target), len(children))
	}

	var doomed []confluence.PageInfo
	for _, child := range children {
		descendants, err := subtree(api, child, removeFolders)
		if err != nil {
			return err
		}
		doomed = append(doomed, descendants...)
	}
	doomed = append(doomed, *target)

	for i := range doomed {
		page := &doomed[i]
		if dryRun {
			fmt.Fprintf(out, "would trash %s\n", describe(page))
			continue
		}
		if err := api.DeletePage(page.ID); err != nil {
			return fmt.Errorf("unable to trash %s, after trashing %d page(s): %w", describe(page), i, err)
		}
		fmt.Fprintf(out, "trashed %s\n", describe(page))
	}

	return nil
}

// subtree is page and everything beneath it, deepest first. A folder anywhere
// in it is refused, for beyondFolder.
func subtree(api *confluence.API, page confluence.PageInfo, beyondFolder string) ([]confluence.PageInfo, error) {
	children, err := directChildren(api, &page, beyondFolder)
	if err != nil {
		return nil, err
	}

	var all []confluence.PageInfo
	for _, child := range children {
		descendants, err := subtree(api, child, beyondFolder)
		if err != nil {
			return nil, err
		}
		all = append(all, descendants...)
	}

	return append(all, page), nil
}

// directChildren lists the pages directly beneath page, and refuses one that
// holds a folder, for beyondFolder. The listing is of pages, so whatever is
// inside a folder is out of its sight: trashing the page would take the folder
// with it, and a recursive watch would miss the pages in it.
func directChildren(api *confluence.API, page *confluence.PageInfo, beyondFolder string) ([]confluence.PageInfo, error) {
	children, err := api.GetChildPages(page.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the children of %s: %w", describe(page), err)
	}

	folders, err := api.HasChildFolders(page.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the children of %s: %w", describe(page), err)
	}
	if folders {
		return nil, fmt.Errorf("%s has a folder beneath it, and %s", describe(page), beyondFolder)
	}

	return children, nil
}

// checkRemovable refuses the space homepage, which holds the whole tree.
func checkRemovable(api *confluence.API, page *confluence.PageInfo) error {
	if page.Type == "blogpost" {
		return nil
	}

	home, err := api.FindHomePage(page.Space.Key)
	if errors.Is(err, confluence.ErrNoHomePage) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to tell whether %s is the homepage of space %s: %w", describe(page), page.Space.Key, err)
	}
	if home != nil && home.ID == page.ID {
		return fmt.Errorf("%s is the homepage of space %s, and mark does not remove it", describe(page), page.Space.Key)
	}

	return nil
}

// parentOf is the id of the page directly above page, or "".
func parentOf(page *confluence.PageInfo) string {
	if page.ParentID != "" {
		return page.ParentID
	}
	if n := len(page.Ancestors); n > 0 {
		return page.Ancestors[n-1].ID
	}
	return ""
}

func describe(page *confluence.PageInfo) string {
	return fmt.Sprintf("%q (%s)", page.Title, page.ID)
}

// Watch makes users watch a page, or stop watching it when watch is false,
// so that Confluence notifies them of its changes; recursive does the same
// for every page beneath it, from the top down. No users means the
// authenticated one; anyone else is named as restrictions name a user, and
// takes space administration.
func Watch(api *confluence.API, ref Ref, users []string, watch, recursive, dryRun bool, out io.Writer) error {
	target, err := Resolve(api, ref)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		users = []string{""}
	}

	pages := []confluence.PageInfo{*target}
	if recursive {
		// The whole tree is read before anything changes, so a folder deep
		// inside it refuses the run rather than stopping it halfway.
		pages, err = subtree(api, *target, "a recursive watch does not reach into folders")
		if err != nil {
			return err
		}
		slices.Reverse(pages)
	}

	verb, state := "watch", "watching"
	if !watch {
		verb, state = "unwatch", "not watching"
	}

	for i := range pages {
		page := &pages[i]
		for _, user := range users {
			who, subject := "you are", "you"
			if user != "" {
				who, subject = fmt.Sprintf("%q is", user), fmt.Sprintf("%q", user)
			}

			watching, err := api.IsWatching(page.ID, user)
			if err != nil {
				return fmt.Errorf("unable to read who watches %s: %w", describe(page), err)
			}
			if watching == watch {
				fmt.Fprintf(out, "%s already %s %s\n", who, state, describe(page))
				continue
			}

			if dryRun {
				fmt.Fprintf(out, "would %s %s for %s\n", verb, describe(page), subject)
				continue
			}

			if watch {
				err = api.WatchPage(page.ID, user)
			} else {
				err = api.UnwatchPage(page.ID, user)
			}
			if err != nil {
				return fmt.Errorf("unable to %s %s for %s: %w", verb, describe(page), subject, err)
			}
			fmt.Fprintf(out, "%sed %s for %s\n", verb, describe(page), subject)
		}
	}

	return nil
}
