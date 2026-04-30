package mmdb

import (
	"bytes"
	"errors"
	"net"
)

const (
	metadataMarker    = "\xab\xcd\xefMaxMind.com"
	dataSeparatorSize = 16
)

// Reader provides country code lookups against a MaxMind DB loaded in memory.
// Safe for concurrent reads after construction.
type Reader struct {
	data       []byte
	nodeCount  uint32
	recordBits uint32 // bits per record: 24, 28, or 32
	nodeBytes  uint32 // bytes per full node (recordBits * 2 / 8)
	ipVersion  uint32
	dataStart  uint32
	ipv4Start  uint32 // starting tree node for IPv4 in IPv6 databases
}

// mmdbMeta holds the three fields we need from the metadata map.
type mmdbMeta struct {
	nodeCount  uint32
	recordSize uint32
	ipVersion  uint32
}

// Open parses an in-memory MaxMind DB binary and returns a Reader.
func Open(data []byte) (*Reader, error) {
	marker := []byte(metadataMarker)
	idx := bytes.LastIndex(data, marker)
	if idx < 0 {
		return nil, errors.New("mmdb: metadata marker not found")
	}

	meta, err := parseMetadata(data[idx+len(marker):])
	if err != nil {
		return nil, err
	}
	if meta.nodeCount == 0 || meta.recordSize == 0 {
		return nil, errors.New("mmdb: metadata missing node_count or record_size")
	}

	nodeBytes := meta.recordSize * 2 / 8
	treeSize := meta.nodeCount * nodeBytes

	if uint64(treeSize)+dataSeparatorSize > uint64(len(data)) {
		return nil, errors.New("mmdb: data too short for search tree")
	}

	r := &Reader{
		data:       data,
		nodeCount:  meta.nodeCount,
		recordBits: meta.recordSize,
		nodeBytes:  nodeBytes,
		ipVersion:  meta.ipVersion,
		dataStart:  treeSize + dataSeparatorSize,
	}

	// In IPv6 databases, IPv4 addresses live in a subtree reached by following
	// 96 zero-bit edges from the root.
	if r.ipVersion == 6 {
		node := uint32(0)
		for i := 0; i < 96; i++ {
			if node >= r.nodeCount {
				break
			}
			node = r.readRecord(node, 0)
		}
		r.ipv4Start = node
	}

	return r, nil
}

// Close is a no-op; the Reader holds no file handles.
func (r *Reader) Close() error { return nil }

// LookupCountryCode traverses the Patricia trie and returns the ISO 3166-1
// alpha-2 country code for ip, or "" when the IP is not found.
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

	// node == nodeCount → IP not in database
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

	return decodeCountry(dataSection, dataOffset)
}

// readRecord reads the left (bit=0) or right (bit=1) record from node n.
func (r *Reader) readRecord(n uint32, bit byte) uint32 {
	base := int(n) * int(r.nodeBytes)
	end := base + int(r.nodeBytes)
	if end > len(r.data) {
		return r.nodeCount
	}
	b := r.data[base:end]
	switch r.recordBits {
	case 24:
		if bit == 0 {
			return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
		}
		return uint32(b[3])<<16 | uint32(b[4])<<8 | uint32(b[5])
	case 28:
		// Bytes 0-2 = low 24 bits of left record.
		// Byte 3 high nibble = bits 27-24 of left, low nibble = bits 27-24 of right.
		// Bytes 4-6 = low 24 bits of right record.
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

// parseMetadata scans the metadata bytes for the three fields we need.
func parseMetadata(meta []byte) (mmdbMeta, error) {
	var result mmdbMeta

	typeCode, low5, offset, err := readCtrl(meta, 0)
	if err != nil {
		return result, err
	}
	if typeCode != mmdbTypeMap {
		return result, errors.New("mmdb: metadata is not a map")
	}
	count, offset, err := readSize(meta, low5, offset)
	if err != nil {
		return result, err
	}

	for i := 0; i < count; i++ {
		key, next, err := readString(meta, offset, meta, 0)
		if err != nil {
			return result, err
		}
		offset = next

		switch key {
		case "node_count":
			v, next, err := readUint32(meta, offset, meta, 0)
			if err != nil {
				return result, err
			}
			result.nodeCount = v
			offset = next
		case "record_size":
			v, next, err := readUint32(meta, offset, meta, 0)
			if err != nil {
				return result, err
			}
			result.recordSize = v
			offset = next
		case "ip_version":
			v, next, err := readUint32(meta, offset, meta, 0)
			if err != nil {
				return result, err
			}
			result.ipVersion = v
			offset = next
		default:
			offset, err = skipValue(meta, offset, 0)
			if err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

// decodeCountry extracts the country ISO code from the data record at offset.
// section is the full data section used for pointer resolution.
func decodeCountry(section []byte, offset int) string {
	typeCode, low5, offset, err := readCtrl(section, offset)
	if err != nil {
		return ""
	}
	if typeCode == mmdbTypePointer {
		target, _, err := ptrTarget(section, low5, offset)
		if err != nil || target >= len(section) {
			return ""
		}
		return decodeCountry(section, target)
	}
	if typeCode != mmdbTypeMap {
		return ""
	}
	count, offset, err := readSize(section, low5, offset)
	if err != nil {
		return ""
	}

	for i := 0; i < count; i++ {
		key, next, err := readString(section, offset, section, 0)
		if err != nil {
			return ""
		}
		offset = next

		switch key {
		case "country", "registered_country", "represented_country":
			code := findISOCode(section, offset)
			offset, err = skipValue(section, offset, 0)
			if err != nil {
				return ""
			}
			if code != "" {
				return code
			}
		default:
			offset, err = skipValue(section, offset, 0)
			if err != nil {
				return ""
			}
		}
	}
	return ""
}

// findISOCode reads the "iso_code" string from the map at offset.
func findISOCode(section []byte, offset int) string {
	typeCode, low5, offset, err := readCtrl(section, offset)
	if err != nil {
		return ""
	}
	if typeCode == mmdbTypePointer {
		target, _, err := ptrTarget(section, low5, offset)
		if err != nil || target >= len(section) {
			return ""
		}
		return findISOCode(section, target)
	}
	if typeCode != mmdbTypeMap {
		return ""
	}
	count, offset, err := readSize(section, low5, offset)
	if err != nil {
		return ""
	}

	for i := 0; i < count; i++ {
		key, next, err := readString(section, offset, section, 0)
		if err != nil {
			return ""
		}
		offset = next

		if key == "iso_code" {
			code, _, err := readString(section, offset, section, 0)
			if err != nil {
				return ""
			}
			return code
		}
		offset, err = skipValue(section, offset, 0)
		if err != nil {
			return ""
		}
	}
	return ""
}
