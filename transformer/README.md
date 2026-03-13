# GitHub Alerts Transformer

This directory contains the GitHub Alerts transformer that enables Mark to convert GitHub-style alert syntax into Confluence macros.

## Overview

The GitHub Alerts transformer processes markdown with GitHub Alert syntax like `[!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!CAUTION]`, and `[!IMPORTANT]` and converts them into appropriate Confluence structured macros.

## Supported Alert Types

| GitHub Alert | Confluence Macro | Description |
|--------------|-----------------|-------------|
| `[!NOTE]` | `info` | General information |
| `[!TIP]` | `tip` | Helpful suggestions |
| `[!IMPORTANT]` | `info` | Critical information |
| `[!WARNING]` | `note` | Important warnings |
| `[!CAUTION]` | `warning` | Dangerous situations |

## Usage Example

### Input Markdown

```markdown
# Test GitHub Alerts

## Note Alert

> [!NOTE]
> This is a note alert with **markdown** formatting.
> 
> - Item 1
> - Item 2

## Tip Alert

> [!TIP]
> This is a tip alert.

## Warning Alert  

> [!WARNING]
> This is a warning alert.

## Regular Blockquote

> This is a regular blockquote without GitHub Alert syntax.
```

### Output (Confluence Storage Format)

The transformer converts GitHub Alert syntax into Confluence structured macros:

```xml
<ac:structured-macro ac:name="info">
  <ac:parameter ac:name="icon">true</ac:parameter>
  <ac:rich-text-body>
    <p>Note This is a note alert with <strong>markdown</strong> formatting.</p>
    <ul>
      <li>Item 1</li>
      <li>Item 2</li>
    </ul>
  </ac:rich-text-body>
</ac:structured-macro>
```

## Key Features

- **GitHub Compatibility**: Full support for GitHub's alert syntax
- **Markdown Preservation**: All markdown formatting within alerts is preserved
- **Fallback Support**: Regular blockquotes without alert syntax remain unchanged
- **User-Friendly Labels**: Adds readable labels (Note, Tip, Warning, etc.) to alert content
- **Confluence Integration**: Maps to appropriate Confluence macro types for optimal display

## Implementation

The transformer works by:

1. **AST Transformation**: Modifies the goldmark AST before rendering
2. **Pattern Matching**: Identifies GitHub Alert patterns in blockquotes
3. **Content Enhancement**: Adds user-friendly labels and processes nested markdown
4. **Macro Generation**: Converts to appropriate Confluence structured macros

## Backward Compatibility

- Legacy `info:`, `tip:`, `warning:` syntax continues to work
- Regular blockquotes remain unchanged
- Full compatibility with existing Mark features

## Testing

The transformer is thoroughly tested with:
- All GitHub Alert types (`[!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!CAUTION]`, `[!IMPORTANT]`)
- Nested markdown formatting (bold, italic, lists, etc.)
- Mixed content scenarios
- Backward compatibility with legacy syntax
- Edge cases and error conditions

See `../markdown/transformer_comparison_test.go` for comprehensive test coverage.
