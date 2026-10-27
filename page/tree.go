package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
)

// Node is a page, folder or other content, and what is beneath it.
type Node struct {
	confluence.Child
	Children []*Node
}

// ReadTree reads what is beneath root, depth levels down; 0 is no limit.
func ReadTree(ctx context.Context, api *confluence.API, root confluence.Child, depth int) (*Node, error) {
	node := &Node{Child: root}
	if depth == 1 || (root.Type != "page" && root.Type != "folder") {
		return node, nil
	}

	children, err := api.Children(ctx, root.ID, root.Type)
	if err != nil {
		return nil, fmt.Errorf("unable to list the children of %q (%s): %w", root.Title, root.ID, err)
	}

	for _, child := range children {
		next := 0
		if depth > 0 {
			next = depth - 1
		}
		sub, err := ReadTree(ctx, api, child, next)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, sub)
	}

	return node, nil
}

// Tree prints the page, or folder, a Ref names and what is beneath it, depth
// levels deep counting the page itself; 0 is no limit.
func Tree(ctx context.Context, api *confluence.API, ref Ref, depth int, out io.Writer) error {
	root, err := resolveChild(ctx, api, ref)
	if err != nil {
		return err
	}

	tree, err := ReadTree(ctx, api, root, depth)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, label(tree.Child))
	printChildren(out, tree, "")

	return nil
}

func printChildren(out io.Writer, node *Node, indent string) {
	for i, child := range node.Children {
		branch, next := "├── ", "│   "
		if i == len(node.Children)-1 {
			branch, next = "└── ", "    "
		}
		fmt.Fprintln(out, indent+branch+label(child.Child))
		printChildren(out, child, indent+next)
	}
}

// label is a line of the tree: the title and id, and the type when it is not
// a page.
func label(child confluence.Child) string {
	if child.Type == "page" {
		return fmt.Sprintf("%s (%s)", child.Title, child.ID)
	}
	return fmt.Sprintf("%s [%s] (%s)", child.Title, child.Type, child.ID)
}

// resolveChild reads the page a Ref names, or the folder its id names.
func resolveChild(ctx context.Context, api *confluence.API, ref Ref) (confluence.Child, error) {
	page, err := Resolve(ctx, api, ref)
	if err == nil {
		return confluence.Child{ID: page.ID, Type: page.Type, Title: page.Title}, nil
	}
	if ref.ID == "" {
		return confluence.Child{}, err
	}

	folder, folderErr := api.GetFolderByID(ctx, ref.ID)
	if folderErr != nil {
		return confluence.Child{}, err
	}

	return confluence.Child{ID: folder.ID, Type: "folder", Title: folder.Title}, nil
}

// CopyOptions says where Copy puts the copy and what it is called. Title
// names the top copy, and Prefix goes before the title of every copy Title
// does not name. Recursive copies the pages beneath it too.
type CopyOptions struct {
	Parent    Ref
	Title     string
	Prefix    string
	Recursive bool
	DryRun    bool
}

// Copy copies a page under another, with its attachments, labels and content
// properties but not its restrictions. With Recursive, the pages beneath it
// are copied too, each under the copy of its parent.
//
// The whole tree is read, and every title the copies would take checked,
// before anything is copied: a title already taken stops the run before the
// first copy rather than halfway.
func Copy(ctx context.Context, api *confluence.API, ref Ref, options CopyOptions, out io.Writer) error {
	source, err := Resolve(ctx, api, ref)
	if err != nil {
		return err
	}
	if source.Type == "blogpost" {
		return fmt.Errorf("%s is a blog post, and blog posts have no place in the page tree", describe(source))
	}

	parent, err := Resolve(ctx, api, options.Parent)
	if err != nil {
		return err
	}
	if parent.Type == "blogpost" {
		return fmt.Errorf("%s is a blog post, and a page cannot go beneath one", describe(parent))
	}

	depth := 1
	if options.Recursive {
		depth = 0
		if parent.ID == source.ID || isAncestor(source.ID, parent) {
			return fmt.Errorf("cannot copy %s with everything beneath it into itself", describe(source))
		}
	}

	tree, err := ReadTree(ctx, api, confluence.Child{ID: source.ID, Type: source.Type, Title: source.Title}, depth)
	if err != nil {
		return err
	}

	titles := map[*Node]string{}
	if err := planTitles(tree, options, titles, true); err != nil {
		return err
	}
	if err := checkTitles(ctx, api, parent.Space.Key, titles); err != nil {
		return err
	}

	if options.DryRun {
		printCopy(out, tree, titles, describe(parent))
		return nil
	}

	copied := 0
	return copyNode(ctx, api, tree, parent.ID, describe(parent), titles, &copied, out)
}

// planTitles works out the title of each copy.
func planTitles(node *Node, options CopyOptions, titles map[*Node]string, top bool) error {
	if node.Type != "page" {
		return fmt.Errorf("%s is beneath the page, and copy copies pages only", label(node.Child))
	}

	titles[node] = options.Prefix + node.Title
	if top && options.Title != "" {
		titles[node] = options.Title
	}

	for _, child := range node.Children {
		if err := planTitles(child, options, titles, false); err != nil {
			return err
		}
	}

	return nil
}

// checkTitles refuses titles that a page in space already has, or that two
// copies would share.
func checkTitles(ctx context.Context, api *confluence.API, space string, titles map[*Node]string) error {
	var taken []string
	seen := map[string]bool{}

	for _, title := range titles {
		key := strings.ToLower(title)
		if seen[key] {
			taken = append(taken, fmt.Sprintf("%q (twice among the copies)", title))
			continue
		}
		seen[key] = true

		existing, err := api.FindPage(ctx, space, title, "page")
		if err != nil {
			return fmt.Errorf("unable to look for a page titled %q in space %s: %w", title, space, err)
		}
		if existing != nil {
			taken = append(taken, fmt.Sprintf("%q (%s)", title, existing.ID))
		}
	}

	if len(taken) > 0 {
		return errors.New("space " + space + " already has the title of a copy: " + strings.Join(taken, ", ") +
			"; give the copy another with --title, or every copy one with --prefix")
	}

	return nil
}

func printCopy(out io.Writer, node *Node, titles map[*Node]string, under string) {
	fmt.Fprintf(out, "would copy %q (%s) as %q under %s\n", node.Title, node.ID, titles[node], under)
	for _, child := range node.Children {
		printCopy(out, child, titles, fmt.Sprintf("the copy of %q", node.Title))
	}
}

func copyNode(
	ctx context.Context,
	api *confluence.API,
	node *Node,
	parentID, under string,
	titles map[*Node]string,
	copied *int,
	out io.Writer,
) error {
	made, err := api.CopyPage(ctx, node.ID, parentID, titles[node])
	if err != nil {
		return fmt.Errorf("unable to copy %q (%s) under %s, after copying %d page(s): %w",
			node.Title, node.ID, under, *copied, err)
	}
	*copied++
	fmt.Fprintf(out, "copied %q (%s) as %s under %s\n", node.Title, node.ID, describe(made), under)

	for _, child := range node.Children {
		if err := copyNode(ctx, api, child, made.ID, describe(made), titles, copied, out); err != nil {
			return err
		}
	}

	return nil
}

func isAncestor(id string, page *confluence.PageInfo) bool {
	for _, ancestor := range page.Ancestors {
		if ancestor.ID == id {
			return true
		}
	}
	return false
}
