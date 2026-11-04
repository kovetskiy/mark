package plantuml

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The example the vendor's "Programmatically adding PlantUML diagrams" page
// carries: the "data" parameter of its sample macro, and the markup that value
// decodes to. The markup is the page's own, written with CRLF line endings and
// no trailing newline; the constant below keeps LF only because a raw string
// literal cannot spell \r\n, and the test reassembles the CRLF spelling.
//
// Together the pair is what the add-on's renderer actually reads on the other
// end of a published page, so a codec that round-trips it interoperates with
// the implementation that wrote it.
const officialData = `pVdNb+IwEP01kbIHEIJWPfdL2sNWPbC715VxJuDi2MF2ULn0t+84CWloZtzVVgIDfs9jj+fNTMiuFj4IF5pKZ4uHbHHbjbWQe7GFbLlogtL4kd3cjXGphfc4va5BXqD4/f11h+NR6AY4UJkCXqfgbDadK5UO4PJDI0zw36Z4LQLiJicgb12g5rcQOnsEthOexebz6VywezDnFTizimj7c8o9gvPKkketIOxsQSHhVAM1r0i2t42TJJ86vCiK3LeRvOd8DnYdnDLbweTNw6USsuUyimH+JGr8mtZEhZz2giLnbIgjx8P1N/YbZLCOOF3HlDbyzmSWNriCY/XRnYlTQ0yjTyJOHg4J75iQI7KxxYkGhKMTBF4Debo+6ebZ6rE7zvj+LqnveVzZAohEVgazphQyUtYnI8VGA+feDN9/tA8mu77Lrh8YgioYoI8Lg+6sD2khRIVSiu7rDJ9UHQHdV0g6Cx3rEo7RG36FKst8IzywDFHX+pS3JY6qSUMFVIUI8OwK3J63JXXCu97GwOBSYAdy/6vWyux5Y1JbT9aGDt4mblnbLX+EtyxWdEXW2rc2AqVoNAuDc7ztqBGo0PZF+LyThLHIpZUQEQc8Zg1dNFsMxcCbHIF0KXma5N/HDLA13m46vK0i2eDEZF+HqBIfR5bXmH9lFspj+9c/RlGf7mpfLI9i207I2otjQtIpPWXLVVwPIS2ZRKg/jdgaAhevPlFw9z2c+IzGzoV41CmKlqU5qLBxJS3FW7xNXaS0WmNzHBdA2qnvscwmvbKbF7TkLxr0imWj2fDTJwjBr+GQwLEYyf1zWWIoE6wasLzTR2JvxBj0I499hb+2Ubkcx+irPYaXxFEoHXtst6nPk8qIAvtEOlvMUNwQg5ZId4pGJgWXbV+oqP9XNc/VctbUs+zmvnvSGZ5PuqegNj2RMODdohbsZT7TUAZmPblvdrUAU+Cfob8=`

const officialMarkupLF = `@startuml

package util {

class Spec {
    + value
    + index
    --
    filter(quants)
    pattern()
    sort()
    get(quant)
    has(quant)
    ..
    token(quant) : quant
    version()
    method()
    type()
    id()
    source()
    ..
    add(spec,quant)
    toString()
}
class "Spec.Map" {
    + map: {}

    + add(versionVector)
    + covers(version)
    + toString(trim)
}

class "(quant)" as qq {
    +quant
    +body
    +bare
    +ext
}

Spec .> qq

}

package model {

interface Syncable {
    - _lstn[]
    - _id
    - _version
    - _host

    + spec()
    + version()
    + deliver(spec,val,lstn)
    + diff(base)
    + apply(values)
    + validateOrder()
    + acl()
    + validate()

    + checkUplink()
    + close()
    + gc()
    + log()

    ~ init()
    ~ default()
    ~ err()

    - emit(spec,val,src)
    - on()
    - reon()
    - once()
    - off()
    - reoff()
}

class Model {
    - _oplog

    + diff()
    + packState(state)
    + unpackState(state)
    + distillLog()
    + pojo()
    + fill()
    + save()

    ~ init()
    # set()

    - on()
    - off()
}

class Set {
    + get(key)
    + add(key,spec)
    + remove(key)
    + fillAll()
    + collection()
}

class Host {
    + objects: {};
    + lastTs;
    + tsSeq;
    + clockOffset;
    + peers: {};

    + connect(host)
    + checkUplink(spec)

    + version()
    + deliver()
    + availableUplinks(spec)
    + get(spec)
    + register(obj)
    + unregister(obj)

    - init()
    - on()
    - reon()
    - off()
    - reoff()
}

Model -up-|> Syncable
Set -up|> Model
Host -left|> Syncable

}

@enduml`

// inflate returns the percent-encoded markup layer under the base64, undoing
// the two transport steps the renderer also undoes.
func inflate(t *testing.T, data string) string {
	t.Helper()

	deflated, err := base64.StdEncoding.DecodeString(data)
	assert.NoError(t, err)

	inflated, err := io.ReadAll(flate.NewReader(bytes.NewReader(deflated)))
	assert.NoError(t, err)

	return string(inflated)
}

// percentDecode is decodeURIComponent: every % must introduce a two-digit hex
// escape, because that is all the codec under test ever writes.
func percentDecode(t *testing.T, escaped string) string {
	t.Helper()

	var raw []byte
	for i := 0; i < len(escaped); {
		if escaped[i] != '%' {
			raw = append(raw, escaped[i])
			i++
			continue
		}

		if i+2 >= len(escaped) {
			t.Fatalf("dangling percent escape at %d in %q", i, escaped)
		}

		hi := strings.IndexByte(upperhex, escaped[i+1])
		lo := strings.IndexByte(upperhex, escaped[i+2])
		if hi < 0 || lo < 0 {
			t.Fatalf("malformed percent escape at %d in %q", i, escaped)
		}

		raw = append(raw, byte(hi<<4|lo))
		i += 3
	}

	return string(raw)
}

// decodeData undoes EncodeData the way the add-on's renderer does: base64,
// raw inflate, decodeURIComponent. Round-tripping through it proves the bytes
// the renderer will read, not just the bytes Go wrote.
func decodeData(t *testing.T, data string) string {
	t.Helper()

	return percentDecode(t, inflate(t, data))
}

// TestEncodeDataReadsTheVendorExample is the interoperability test: the
// example macro the vendor publishes must decode through mark's own steps,
// and mark's encoding of the same markup must produce the same percent layer
// -- the layer is the interoperable part, the deflate stream below it is an
// implementation detail whose bytes differ between compressors.
func TestEncodeDataReadsTheVendorExample(t *testing.T) {
	markup := strings.ReplaceAll(officialMarkupLF, "\n", "\r\n")

	// Their output, read by mark's decoder.
	assert.Equal(t, markup, decodeData(t, officialData))

	// Mark's output, read the same way. The base64 need not equal the
	// vendor's -- deflate output depends on the compressor -- but the layer
	// it inflates to has to match theirs byte for byte.
	data, err := EncodeData([]byte(markup))
	assert.NoError(t, err)
	assert.Equal(t, markup, decodeData(t, data))

	layer := inflate(t, data)
	assert.Equal(t, inflate(t, officialData), layer)
	assert.Equal(t, percentEncode(markup), layer)
}

// TestProcess covers the filename parameter's naming rule: the info string's
// title, the markup's checksum without one.
func TestProcess(t *testing.T) {
	markup := []byte("@startuml\nAlice -> Bob: hello\n@enduml")

	t.Run("titled diagram", func(t *testing.T) {
		macro, err := Process("Alice and Bob", markup)
		assert.NoError(t, err)
		assert.Equal(t, "Alice and Bob.svg", macro.Filename)
		assert.Equal(t, string(markup), decodeData(t, macro.Data))
	})

	t.Run("untitled diagram falls back to the checksum", func(t *testing.T) {
		checksum := sha256.Sum256(markup)

		macro, err := Process("", markup)
		assert.NoError(t, err)
		assert.Equal(t, hex.EncodeToString(checksum[:])+".svg", macro.Filename)
	})

	t.Run("a slash in a title is flattened", func(t *testing.T) {
		macro, err := Process("roadmap/2027", markup)
		assert.NoError(t, err)
		assert.Equal(t, "roadmap_2027.svg", macro.Filename)
	})
}

func TestPercentEncode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		markup string
		want   string
	}{
		{
			// The renderer runs decodeURIComponent over the layer, so a
			// literal % in the markup has to arrive as %25 or it is read
			// back as the character its %XX happens to spell.
			name:   "literal percent",
			markup: "100%20visible",
			want:   "100%2520visible",
		},
		{
			// !'()* pass through, which is what QueryEscape gets wrong.
			name:   "punctuation encodeURIComponent keeps",
			markup: "It's (100%) ~ok~.!*'",
			want:   "It's%20(100%25)%20~ok~.!*'",
		},
		{
			name:   "non-ASCII per UTF-8 byte",
			markup: "日本語",
			want:   "%E6%97%A5%E6%9C%AC%E8%AA%9E",
		},
		{
			name:   "reserved characters",
			markup: `Alice -> Bob: 50% & "quoted"`,
			want:   "Alice%20-%3E%20Bob%3A%2050%25%20%26%20%22quoted%22",
		},
		{
			name:   "the whole unreserved set",
			markup: "AZaz09-_.!~*'()",
			want:   "AZaz09-_.!~*'()",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, percentEncode(tc.markup))
		})
	}
}

func TestEncodeDataRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		markup string
	}{
		{
			name: "sequence diagram",
			markup: "@startuml\nAlice -> Bob: Authentication Request\n" +
				"Bob --> Alice: Authentication Response\n@enduml",
		},
		{
			// A Gantt label measured in percent is where a raw %XX would
			// otherwise land.
			name:   "literal percent in a label",
			markup: "@startuml\nobject progress {\n100%20visible\n}\n@enduml",
		},
		{
			name:   "non-ASCII markup",
			markup: "@startuml\nAlice -> Bob: こんにちは、世界\n@enduml",
		},
		{
			name:   "CRLF line endings",
			markup: "@startuml\r\nAlice -> Bob: ok\r\n@enduml",
		},
		{
			name:   "empty markup",
			markup: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := EncodeData([]byte(tc.markup))
			assert.NoError(t, err)
			assert.NotEmpty(t, data)

			assert.Equal(t, tc.markup, decodeData(t, data))
		})
	}
}
