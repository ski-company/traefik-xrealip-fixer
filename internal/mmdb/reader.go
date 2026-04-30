package mmdb

import (
	"bytes"
	"errors"
	"fmt"
	"net"
)

const (
	metadataMarker    = "\xab\xcd\xefMaxMind.com"
	dataSeparatorSize = 16
)

// Reader provides country code lookups against a MaxMind DB loaded in memory.
// It is safe for concurrent use after construction.
type Reader struct {
	data       []byte
	nodeCount  uint32
	recordBits uint32 // bits per record: 24, 28, or 32
	nodeBytes  uint32 // bytes per full node (= recordBits * 2 / 8)
	ipVersion  uint32
	dataStart  uint32
	ipv4Start  uint32 // starting tree node for IPv4 lookups in IPv6 databases
}

// Open parses an in-memory MaxMind DB and returns a Reader.
func Open(data []byte) (*Reader, error) {
	marker := []byte(metadataMarker)
	idx := bytes.LastIndex(data, marker)
	if idx < 0 {
		return nil, errors.New("mmdb: metadata marker not found")
	}

	metaBytes := data[idx+len(marker):]
	raw, _, err := decodeValue(metaBytes, 0, metaBytes)
	if err != nil {
		return nil, fmt.Errorf("mmdb: decode metadata: %w", err)
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.New("mmdb: metadata is not a map")
	}

	nodeCount, ok1 := asUint32(m["node_count"])
	recordBits, ok2 := asUint32(m["record_size"])
	if !ok1 || !ok2 {
		return nil, errors.New("mmdb: metadata missing node_count or record_size")
	}
	ipVersion, _ := asUint32(m["ip_version"])

	nodeBytes := recordBits * 2 / 8
	treeSize := nodeCount * nodeBytes

	r := &Reader{
		data:       data,
		nodeCount:  nodeCount,
		recordBits: recordBits,
		nodeBytes:  nodeBytes,
		ipVersion:  ipVersion,
		dataStart:  treeSize + dataSeparatorSize,
	}

	// In IPv6 databases, IPv4 addresses live in a subtree reached by following
	// 96 left (zero-bit) edges from the root.
	if ipVersion == 6 {
		node := uint32(0)
		for i := 0; i < 96 && node < r.nodeCount; i++ {
			node = r.readRecord(node, 0)
		}
		r.ipv4Start = node
	}

	return r, nil
}

// Close is a no-op; the Reader holds no file handles.
func (r *Reader) Close() error { return nil }

// LookupCountryCode traverses the Patricia trie and returns the ISO 3166-1
// alpha-2 country code for ip. Returns "" when the IP is not found.
func (r *Reader) LookupCountryCode(ip net.IP) string {
	var (
		bits  []byte
		start uint32
	)
	if ip4 := ip.To4(); ip4 != nil {
		bits = ip4
		if r.ipVersion == 6 {
			start = r.ipv4Start
		}
	} else if ip6 := ip.To16(); ip6 != nil {
		bits = ip6
	} else {
		return ""
	}

	node := start
	for i := 0; i < len(bits)*8; i++ {
		if node >= r.nodeCount {
			break
		}
		bit := (bits[i/8] >> (7 - uint(i%8))) & 1
		node = r.readRecord(node, bit)
	}

	// node == nodeCount means the IP is not in the database.
	if node <= r.nodeCount {
		return ""
	}

	// data_record_offset = node - nodeCount - 16 (separator size)
	dataOffset := int(node-r.nodeCount) - dataSeparatorSize
	if dataOffset < 0 {
		return ""
	}

	dataSection := r.data[r.dataStart:]
	if dataOffset >= len(dataSection) {
		return ""
	}

	raw, _, err := decodeValue(dataSection, dataOffset, dataSection)
	if err != nil {
		return ""
	}
	rec, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	return extractCountryCode(rec)
}

// readRecord reads one record (left when bit=0, right when bit=1) from node n.
func (r *Reader) readRecord(n uint32, bit byte) uint32 {
	base := int(n) * int(r.nodeBytes)
	b := r.data[base:]
	switch r.recordBits {
	case 24:
		if bit == 0 {
			return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
		}
		return uint32(b[3])<<16 | uint32(b[4])<<8 | uint32(b[5])
	case 28:
		// Layout: bytes 0-2 = low 24 bits of left, byte 3 = high nibbles of
		// both records, bytes 4-6 = low 24 bits of right.
		if bit == 0 {
			return uint32(b[3]>>4)<<24 | uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
		}
		return uint32(b[3]&0x0F)<<24 | uint32(b[4])<<16 | uint32(b[5])<<8 | uint32(b[6])
	case 32:
		if bit == 0 {
			return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		}
		return uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
	}
	return r.nodeCount
}

// extractCountryCode walks the country fields in priority order.
func extractCountryCode(m map[string]interface{}) string {
	for _, key := range []string{"country", "registered_country", "represented_country"} {
		sub, _ := m[key].(map[string]interface{})
		code, _ := sub["iso_code"].(string)
		if code != "" {
			return code
		}
	}
	return ""
}

func asUint32(v interface{}) (uint32, bool) {
	u, ok := v.(uint32)
	return u, ok
}
