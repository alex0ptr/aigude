package userconfig

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// position is a place in a file, 1-based.
type position struct{ line, column int }

// locator knows where each key of a TOML document was written and in which
// order the CastProfiles and their Subscriptions first appear. The positions of
// semantic mistakes come from a second walk of the document with go-toml's
// low-level parser.
type locator struct {
	positions map[string]position
	// order lists, per table path (see pathKey), the keys directly below it in file order.
	order map[string][]string
	// byLine is the key written on each line, to name the key of a decode error
	// that only knows a position.
	byLine map[int][]string
	// repeated are table headers written a second time; the locator supplies
	// the position for go-toml's rejection of the document.
	repeated []repeatedTable
}

// repeatedTable is a table header that appeared more than once.
type repeatedTable struct {
	path []string
	pos  position
}

// pathKey joins a key path into a lookup key. The separator cannot
// occur in an Id; a quoted key that contains it has no position.
func pathKey(parts ...string) string { return strings.Join(parts, "\x1f") }

// element names the n-th element of an array of tables in a key path.
func element(n int) string { return "[" + strconv.Itoa(n) + "]" }

// newLocator walks the document. A syntax error ends the walk quietly: the
// decoder reports it with its own position.
func newLocator(data []byte) *locator {
	l := &locator{positions: map[string]position{}, order: map[string][]string{}, byLine: map[int][]string{}}
	var p unstable.Parser
	p.Reset(data)

	var table []string
	headers := map[string]bool{}
	elements := map[string]int{}
	for p.NextExpression() {
		node := p.Expression()
		switch node.Kind {
		case unstable.Table:
			table = keyParts(node)
			if headers[pathKey(table...)] {
				l.repeated = append(l.repeated, repeatedTable{path: table, pos: keyPosition(&p, node)})
			}
			headers[pathKey(table...)] = true
			l.record(table, keyPosition(&p, node))
		case unstable.ArrayTable:
			array := keyParts(node)
			n := elements[pathKey(array...)]
			elements[pathKey(array...)] = n + 1
			table = append(array, element(n))
			l.record(table, keyPosition(&p, node))
		case unstable.KeyValue:
			full := append(append([]string(nil), table...), keyParts(node)...)
			pos := keyPosition(&p, node)
			l.byLine[pos.line] = full
			l.record(full, pos)
		}
	}
	slog.Debug("Located keys", "keys", len(l.positions))
	return l
}

// keyParts returns the dotted parts of the key of a table header or key/value.
func keyParts(node *unstable.Node) []string {
	var parts []string
	for it := node.Key(); it.Next(); {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

// keyPosition returns where the key of a node starts.
func keyPosition(p *unstable.Parser, node *unstable.Node) position {
	it := node.Key()
	if !it.Next() {
		return position{1, 1}
	}
	shape := p.Shape(it.Node().Raw)
	return position{shape.Start.Line, shape.Start.Column}
}

// record remembers the first position of a path and each of its prefixes, and
// the order in which the keys below each table appear.
func (l *locator) record(path []string, pos position) {
	for n := 1; n <= len(path); n++ {
		key := pathKey(path[:n]...)
		if _, seen := l.positions[key]; seen {
			continue
		}
		l.positions[key] = pos
		if n >= 2 {
			parent := pathKey(path[:n-1]...)
			l.order[parent] = append(l.order[parent], path[n-1])
		}
	}
}

// at returns the position of a key path, falling back to the nearest
// enclosing key that was written, and finally to the start of the file.
func (l *locator) at(path ...string) position {
	for n := len(path); n > 0; n-- {
		if pos, ok := l.positions[pathKey(path[:n]...)]; ok {
			return pos
		}
	}
	return position{1, 1}
}
