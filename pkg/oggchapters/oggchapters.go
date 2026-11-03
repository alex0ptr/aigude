// Package oggchapters writes the Chapters of an Episode into an OGG/Opus file as chapter tags, so ordinary players
// such as VLC and AntennaPod show them.
//
// The tags are Vorbis comments in the OpusTags header of the file (CHAPTER001=00:00:00.000 and CHAPTER001NAME=...).
// The package rewrites the pages of that header itself, from the exact offsets of the Chapters, and leaves the
// audio untouched.
package oggchapters

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// Tagger writes the Chapters into OGG/Opus files.
type Tagger struct{}

// New creates a Tagger.
func New() Tagger { return Tagger{} }

const (
	opusTagsMagic = "OpusTags"
	// maxSegments is how many lacing values one Ogg page holds.
	maxSegments = 255
	// noGranule is the granule position of a page on which no packet ends.
	noGranule = ^uint64(0)
	// continuedPacket is the header type flag of a page that continues the packet of the page before it.
	continuedPacket = 0x01
)

// TagChapters returns the Ogg/Opus file with the Chapters as chapter tags in its comment header. Chapter tags the
// file already has are replaced.
func (Tagger) TagChapters(ogg []byte, chapters []business.Chapter) ([]byte, error) {
	slog.Debug("Writing chapter tags", "bytes", len(ogg), "chapters", len(chapters))
	pages, err := readPages(ogg)
	if err != nil {
		return nil, err
	}
	// The identification header is the first page; the comment header follows on pages of its own.
	if len(pages) < 3 || !bytes.HasPrefix(pages[0].data, []byte("OpusHead")) {
		return nil, errors.New("tag chapters: not an OGG/Opus file")
	}
	last, err := lastPageOfPacket(pages, 1)
	if err != nil {
		return nil, err
	}
	var packet []byte
	for _, p := range pages[1 : last+1] {
		packet = append(packet, p.data...)
	}
	tags, err := withChapters(packet, chapters)
	if err != nil {
		return nil, err
	}
	slog.Debug("Comment header read", "bytes", len(packet), "pages", last, "rewrittenBytes", len(tags))

	tagPages := paginate(tags, pages[1].serial, pages[1].seq)
	shift := len(tagPages) - len(pages[1:last+1])
	slog.Debug("Rewriting the comment header", "pagesBefore", len(pages[1:last+1]), "pagesAfter", len(tagPages))

	var out bytes.Buffer
	out.Write(pages[0].bytes())
	for _, p := range tagPages {
		out.Write(p.bytes())
	}
	for _, p := range pages[last+1:] {
		p.seq = uint32(int(p.seq) + shift)
		out.Write(p.bytes())
	}
	return out.Bytes(), nil
}

// lastPageOfPacket returns the index of the page on which the packet that starts on page first ends. A comment
// header ends on a page of its own, no audio shares it.
func lastPageOfPacket(pages []page, first int) (int, error) {
	for i := first; i < len(pages); i++ {
		lacing := pages[i].lacing
		if lacing[len(lacing)-1] < 255 {
			return i, nil
		}
	}
	return 0, errors.New("tag chapters: the comment header does not end")
}

// withChapters returns the OpusTags packet with the chapter comments in place of the ones it had.
func withChapters(packet []byte, chapters []business.Chapter) ([]byte, error) {
	vendor, comments, err := parseOpusTags(packet)
	if err != nil {
		return nil, err
	}
	var kept []string
	for _, c := range comments {
		if strings.HasPrefix(strings.ToUpper(c), "CHAPTER") {
			slog.Debug("Dropping an existing chapter tag", "tag", c)
			continue
		}
		kept = append(kept, c)
	}
	for i, chapter := range chapters {
		number := fmt.Sprintf("CHAPTER%03d", i+1)
		title := strings.Join(strings.Fields(chapter.Title), " ")
		kept = append(kept, number+"="+formatOffset(chapter.Start), number+"NAME="+title)
	}

	var out bytes.Buffer
	out.WriteString(opusTagsMagic)
	writeString(&out, vendor)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(kept)))
	for _, c := range kept {
		writeString(&out, c)
	}
	return out.Bytes(), nil
}

// writeString writes a length-prefixed string of a Vorbis comment header.
func writeString(out *bytes.Buffer, s string) {
	_ = binary.Write(out, binary.LittleEndian, uint32(len(s)))
	out.WriteString(s)
}

// parseOpusTags reads the vendor and the comments of an OpusTags packet.
func parseOpusTags(packet []byte) (vendor string, comments []string, err error) {
	bad := errors.New("tag chapters: the comment header is damaged")
	if !bytes.HasPrefix(packet, []byte(opusTagsMagic)) {
		return "", nil, errors.New("tag chapters: no OpusTags header after the identification header")
	}
	rest := packet[len(opusTagsMagic):]
	read := func() (string, bool) {
		if len(rest) < 4 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(rest))
		if n < 0 || len(rest)-4 < n {
			return "", false
		}
		s := string(rest[4 : 4+n])
		rest = rest[4+n:]
		return s, true
	}
	vendor, ok := read()
	if !ok || len(rest) < 4 {
		return "", nil, bad
	}
	count := int(binary.LittleEndian.Uint32(rest))
	rest = rest[4:]
	for i := 0; i < count; i++ {
		c, ok := read()
		if !ok {
			return "", nil, bad
		}
		comments = append(comments, c)
	}
	return vendor, comments, nil
}

// formatOffset writes an offset as the chapter tags do: HH:MM:SS.mmm, rounded to the millisecond.
func formatOffset(d time.Duration) string {
	ms := d.Round(time.Millisecond).Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
