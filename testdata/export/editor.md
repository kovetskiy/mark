# Release process

The **release** is cut by <ac:link><ri:user ri:account-id="5b10a2844c20165700ede21g"/></ac:link> every *second* Tuesday, see [the calendar](https://example.com/calendar) and [the rota](<ac:On-call rota>).

Text in <u>underline</u>, <span style="color: rgb(255,86,48);">colour</span>, H<sub>2</sub>O and x<sup>2</sup>, with a `flag` and ~~no~~ strikethrough.\
A second line <ac:emoticon ac:name="tick" ac:emoji-shortname=":white_check_mark:" ac:emoji-id="2705" ac:emoji-fallback="✅"/> after a break.

## Status

<p><ac:structured-macro ac:name="status" ac:schema-version="1" ac:macro-id="0f1a"><ac:parameter ac:name="colour">Green</ac:parameter><ac:parameter ac:name="title">Done</ac:parameter></ac:structured-macro></p>

<ac:structured-macro ac:name="info" ac:schema-version="1" ac:macro-id="a1b2"><ac:rich-text-body>

Freeze starts on <time datetime="2024-03-05"></time>.

</ac:rich-text-body></ac:structured-macro>

```yaml Midnight linenumbers
release:
  day: tuesday
```

<details>
<summary>Checklist &amp; notes</summary>

- [x] <span class="placeholder-inline-tasks">Tag the release</span>
- [ ] Announce it

</details>

## Steps

3. Merge the branch

4. Build

   - linux
   - darwin

| **Platform** | Owner |
| --- | :---: |
| linux \| amd64 | <ac:link><ri:user ri:account-id="abc"/></ac:link> |

<table><tbody><tr><td colspan="2"><p>Merged</p></td></tr><tr><td><p>a</p></td><td><ul><li>b</li></ul></td></tr></tbody></table>

![Pipeline](pipeline%20diagram.png)

<img src="small.png" width="200" />

[Release notes](notes.pdf) and <ac:link><ri:page ri:space-key="OPS" ri:content-title="Runbook"/></ac:link>.

> Ship small, ship often.

---

Reviewed by the team.

```
plain  preformatted
```
