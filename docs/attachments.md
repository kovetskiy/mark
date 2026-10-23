# Attachments and images

## Declaring attachments

Mark also supports attachments. The standard way involves declaring an
`Attachment` along with the other items in the header, then have any links
with the same path:

```markdown
<!-- Attachment: <path-to-image> -->

<beginning of page content>

An attached link is [here](<path-to-image>)
```

A path may be a pattern, in the same syntax `--files` uses, which uploads every
file it matches:

```markdown
<!-- Attachment: images/*.png -->
<!-- Attachment: media/**/*.svg -->
```

Each match is attached under the path it was found at, so a link or image in the
page refers to it exactly as it would have without the pattern —
`![](images/logo.png)`. A pattern is not reported as an unused attachment when
the page does not link to all of it, since uploading a directory is the point of
writing one.

A name with no pattern characters in it is a path, exactly as before. So is one
whose pattern matches nothing: a file really called `report[2024].pdf` still
attaches, and a pattern that was meant to match something reports the path as
written when nothing is there.

A pattern reaches no further than a path does — the document's own directory or
the one mark is running in — so `../../*.pem` is refused rather than swept up.

A link or image destination is a URL, so a character that means something in
one is written percent-encoded: `![](a%23b.png)` for a file called `a#b.png`,
`[notes](100%25.pdf)` for `100%.pdf`, and `my%20file.png` or `<my file.png>` for
`my file.png`. The destination is tried exactly as written first, so a file
really called `my%20file.png` is still found under that name.

An image is uploaded whether or not it is declared: `![](images/logo.png)`
attaches the file and shows it. A **link** to a file is not, and is published as
the path the document wrote -- which means nothing once the page is on
Confluence, so the reader gets a link that leads nowhere.

`--attach-referenced` uploads those too, and links to the attachment:

```markdown
See [the report](files/report.pdf).
```

Only a file that is there, beside the document, and only what a document may
read anyway: the same boundary an attachment is held to, so a link reaching out
of the project is refused rather than published. A URL, an anchor, a mail
address, a rooted path, a name with nothing behind it, and a link to another
document are all left exactly as they are -- the last of those because linking
to a page is not asking to publish its source as a download.

**NOTE**: Be careful with `Attachment`! If your path string is a subset of
another longer string or referenced in text, you may get undesired behavior.

## Upload and included inline images

```markdown
![Example](../images/examples.png)
```

will automatically upload the inlined image as an attachment and inline the image using the `ac:image` template.

The path is read the way Markdown defines it: `my%20file.png` and
`<my file.png>` both name `my file.png`, and the backslash escape in
`my\_file.png` names `my_file.png`. The name as written is tried
first, so a file whose name really contains a `%` still resolves to itself. The alt text
and title are read the same way: `"a \"quoted\" &amp; b"` is published as
`a "quoted" & b`.

If the file is not found, it will inline the image using the `ac:image` template and link to the image.

## Add width for an image

Use the following macro:

```markdown
<!-- Macro: \!\[.*\]\((.+)\)\<\!\-\- width=(.*) \-\-\>
     Template: ac:image
     Attachment: ${1}
     Width: ${2} -->
```

And attach any image with the following

```markdown
![Example](../images/example.png)<!-- width=300 -->
```

The width will be the commented html after the image (in this case 300px).

The file named by `Attachment:` is uploaded with the page, and the name the macro
writes into the page is the one it is uploaded under. A path is read relative to
the document, and is subject to the same rules as any other attachment (see
[Where an attachment may come from](#where-an-attachment-may-come-from)). A file that is also declared with `<!-- Attachment: -->` is uploaded once. A value that is a URL
(`https://...`, `mailto:`, `data:` and the like, though not a filename that merely holds a colon), or an absolute or UNC path, with or without angle brackets, names no file beside the document and is left alone;
one that only becomes such a path once read, as `%2Fetc%2Fpasswd` and `\/etc/passwd` do, is never looked
for and is warned about like a missing file. A percent-encoded name such as `my%20logo.png` is read as it is
for an image: the file of that exact name if there is one, otherwise `my logo.png`.
An image title captured along with the name, as in `![Logo](logo.png "The logo")`,
is not part of it: the name without the title is looked for first, and the value
as written only when that file is not there, so `draft (v2)` names `draft` if there
is one, as it would for an image. A file that is not there is only warned about,
under the name the destination gives it, since it may
already be attached to the page, unlike a file declared with
`<!-- Attachment: -->`, which fails the run.

The macro replaces the whole image, so the native `<img width="300">` syntax
described under [HTML img tags](#use-html-img-tags-for-sizing) needs no macro and is usually the
simpler route.

### Where an attachment may come from

A file is attached when it is inside the directory Mark is running in, or inside
the document's own directory. An upward path is ordinary — `../images/logo.png`
from a `docs` folder is how a repository refers to shared assets — and stays
inside the repository when Mark is run at its root.

A path that resolves outside both, `../../../etc/passwd` or a symlink pointing
out of the repository, fails the file rather than being uploaded. A document
says which files to publish, and on a repository that accepts pull requests a
document is something a contributor writes.

If a legitimate attachment lives outside, publish from a directory that contains
both it and the documents.

## Use HTML img tags for sizing

Standard HTML `<img>` tags (inline, block, single-line, or multi-line) are converted into `<ac:image>` macros. This allows you to specify sizing while keeping the document readable in standard Markdown renderers like GitHub:

```markdown
<img src="../images/example.png" width="300" alt="Example" title="An Example" />
```

Standard attributes (`width`, `alt`, and `title`) are supported and carried over directly. Local image files are uploaded as attachments, and remote image URLs are linked directly.

An `<img>` wrapped in other HTML is converted where it stands, and the markup around it is kept, so the usual centered-image idiom works as it does on GitHub:

```markdown
<p align="center">
  <img src="../images/example.png" width="200">
</p>
```

An `<img>` inside a `<picture>` is left as written, because Confluence has nothing to turn the `<source>` elements beside it into. This conversion applies to the default compile path only; the legacy renderer leaves every `<img>` as written.
