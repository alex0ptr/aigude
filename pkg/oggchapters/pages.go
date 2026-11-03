package oggchapters

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
)

// page is one Ogg page.
type page struct {
	headerType byte
	granule    uint64
	serial     uint32
	seq        uint32
	// lacing are the segment sizes of the page, data the bytes of all segments.
	lacing []byte
	data   []byte
}

const pageHeaderSize = 27

// readPages splits an Ogg stream into its pages.
func readPages(ogg []byte) ([]page, error) {
	var pages []page
	for offset := 0; offset < len(ogg); {
		rest := ogg[offset:]
		if len(rest) < pageHeaderSize || string(rest[:4]) != "OggS" || rest[4] != 0 {
			return nil, fmt.Errorf("tag chapters: not an Ogg page at byte %d", offset)
		}
		segments := int(rest[26])
		if len(rest) < pageHeaderSize+segments {
			return nil, errors.New("tag chapters: truncated Ogg page")
		}
		lacing := rest[pageHeaderSize : pageHeaderSize+segments]
		size := 0
		for _, l := range lacing {
			size += int(l)
		}
		end := pageHeaderSize + segments + size
		if len(rest) < end {
			return nil, errors.New("tag chapters: truncated Ogg page")
		}
		pages = append(pages, page{
			headerType: rest[5],
			granule:    binary.LittleEndian.Uint64(rest[6:]),
			serial:     binary.LittleEndian.Uint32(rest[14:]),
			seq:        binary.LittleEndian.Uint32(rest[18:]),
			lacing:     append([]byte(nil), lacing...),
			data:       append([]byte(nil), rest[pageHeaderSize+segments:end]...),
		})
		offset += end
	}
	if len(pages) == 0 {
		return nil, errors.New("tag chapters: no Ogg pages")
	}
	slog.Debug("Ogg pages read", "pages", len(pages), "bytes", len(ogg))
	return pages, nil
}

// paginate puts one packet on as many pages as it needs, numbered from seq. The pages that do not end the packet
// carry the granule position -1; the last one carries 0, as the pages of the headers do.
func paginate(packet []byte, serial, seq uint32) []page {
	size := len(packet)
	lacing := bytes.Repeat([]byte{255}, len(packet)/255)
	lacing = append(lacing, byte(len(packet)%255))

	var pages []page
	for len(lacing) > 0 {
		n := min(len(lacing), maxSegments)
		taken := 0
		for _, l := range lacing[:n] {
			taken += int(l)
		}
		p := page{granule: noGranule, serial: serial, seq: seq + uint32(len(pages)), lacing: lacing[:n], data: packet[:taken]}
		if len(pages) > 0 {
			p.headerType = continuedPacket
		}
		pages = append(pages, p)
		lacing, packet = lacing[n:], packet[taken:]
	}
	pages[len(pages)-1].granule = 0
	slog.Debug("Packet put on pages", "bytes", size, "pages", len(pages))
	return pages
}

// bytes is the page as it is written to the stream, with its checksum.
func (p page) bytes() []byte {
	out := make([]byte, 0, pageHeaderSize+len(p.lacing)+len(p.data))
	out = append(out, "OggS"...)
	out = append(out, 0, p.headerType)
	out = binary.LittleEndian.AppendUint64(out, p.granule)
	out = binary.LittleEndian.AppendUint32(out, p.serial)
	out = binary.LittleEndian.AppendUint32(out, p.seq)
	out = append(out, 0, 0, 0, 0, byte(len(p.lacing)))
	out = append(out, p.lacing...)
	out = append(out, p.data...)
	binary.LittleEndian.PutUint32(out[22:], checksum(out))
	return out
}

// crcTable is the table of the Ogg checksum: CRC-32 with the polynomial 0x04c11db7, not reflected.
var crcTable = func() (table [256]uint32) {
	for i := range table {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()

// checksum is the Ogg checksum of a page whose checksum field is zero.
func checksum(page []byte) uint32 {
	var crc uint32
	for _, b := range page {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^b]
	}
	return crc
}
