// Package mmdb implements a minimal MaxMind DB reader using only stdlib.
package mmdb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MaxMind DB type codes (high 3 bits of control byte).
const (
	typeExtended = 0
	typePointer  = 1
	typeString   = 2
	typeDouble   = 3
	typeBytes    = 4
	typeUint16   = 5
	typeUint32   = 6
	typeMap      = 7
	// Extended type codes decoded as: second_byte + 7.
	typeInt32   = 8
	typeUint64  = 9
	typeUint128 = 10
	typeArray   = 11
	typeCache   = 12
	typeEnd     = 13
	typeBool    = 14
	typeFloat   = 15
)

// decodeValue decodes one MaxMind DB encoded value at data[offset].
// dataSection is the slice used to resolve pointer offsets — pass the same
// section being decoded (metadata or data) since pointers are self-relative.
func decodeValue(data []byte, offset int, dataSection []byte) (interface{}, int, error) {
	return decodeRec(data, offset, dataSection, 0)
}

func decodeRec(data []byte, offset int, dataSection []byte, depth int) (interface{}, int, error) {
	if depth > 32 {
		return nil, offset, errors.New("mmdb: max decode depth exceeded")
	}
	if offset >= len(data) {
		return nil, offset, fmt.Errorf("mmdb: read past end at offset %d (len %d)", offset, len(data))
	}

	ctrl := data[offset]
	offset++

	typeCode := int(ctrl >> 5)
	if typeCode == typeExtended {
		if offset >= len(data) {
			return nil, offset, errors.New("mmdb: truncated extended type byte")
		}
		typeCode = int(data[offset]) + 7
		offset++
	}

	if typeCode == typePointer {
		return decodePointer(data, ctrl, offset, dataSection, depth)
	}

	size, offset, err := readSize(data, ctrl&0x1f, offset)
	if err != nil {
		return nil, offset, err
	}

	switch typeCode {
	case typeString:
		end := offset + int(size)
		if end > len(data) {
			return nil, offset, errors.New("mmdb: string extends past end of data")
		}
		return string(data[offset:end]), end, nil

	case typeUint16, typeUint32:
		end := offset + int(size)
		if end > len(data) {
			return nil, offset, errors.New("mmdb: uint extends past end of data")
		}
		var v uint32
		for i := offset; i < end; i++ {
			v = (v << 8) | uint32(data[i])
		}
		return v, end, nil

	case typeInt32:
		end := offset + int(size)
		if end > len(data) {
			return nil, offset, errors.New("mmdb: int32 extends past end of data")
		}
		var v int32
		for i := offset; i < end; i++ {
			v = (v << 8) | int32(data[i])
		}
		return v, end, nil

	case typeMap:
		m := make(map[string]interface{}, size)
		for i := uint32(0); i < size; i++ {
			rawKey, next, err := decodeRec(data, offset, dataSection, depth+1)
			if err != nil {
				return nil, next, err
			}
			offset = next
			key, ok := rawKey.(string)
			if !ok {
				return nil, offset, fmt.Errorf("mmdb: map key is not a string (got %T)", rawKey)
			}
			val, next, err := decodeRec(data, offset, dataSection, depth+1)
			if err != nil {
				return nil, next, err
			}
			offset = next
			m[key] = val
		}
		return m, offset, nil

	case typeArray:
		arr := make([]interface{}, 0, size)
		for i := uint32(0); i < size; i++ {
			val, next, err := decodeRec(data, offset, dataSection, depth+1)
			if err != nil {
				return nil, next, err
			}
			offset = next
			arr = append(arr, val)
		}
		return arr, offset, nil

	case typeBool:
		return size != 0, offset, nil

	case typeDouble:
		if offset+8 > len(data) {
			return nil, offset, errors.New("mmdb: double extends past end of data")
		}
		return nil, offset + 8, nil

	case typeFloat:
		if offset+4 > len(data) {
			return nil, offset, errors.New("mmdb: float extends past end of data")
		}
		return nil, offset + 4, nil

	case typeBytes, typeUint64, typeUint128:
		end := offset + int(size)
		if end > len(data) {
			return nil, offset, fmt.Errorf("mmdb: type %d extends past end of data", typeCode)
		}
		return nil, end, nil

	case typeEnd, typeCache:
		return nil, offset, nil

	default:
		return nil, offset, fmt.Errorf("mmdb: unknown type code %d", typeCode)
	}
}

// decodePointer resolves a pointer value and decodes the value it points to.
// Pointer offsets are absolute positions within dataSection.
func decodePointer(data []byte, ctrl byte, offset int, dataSection []byte, depth int) (interface{}, int, error) {
	ptrSize := (ctrl >> 3) & 0x3
	ptrBase := int(ctrl & 0x7)
	var ptrVal int

	switch ptrSize {
	case 0:
		if offset >= len(data) {
			return nil, offset, errors.New("mmdb: truncated pointer (size 0)")
		}
		ptrVal = (ptrBase << 8) | int(data[offset])
		offset++
	case 1:
		if offset+1 >= len(data) {
			return nil, offset, errors.New("mmdb: truncated pointer (size 1)")
		}
		ptrVal = ((ptrBase << 16) | (int(data[offset]) << 8) | int(data[offset+1])) + 2048
		offset += 2
	case 2:
		if offset+2 >= len(data) {
			return nil, offset, errors.New("mmdb: truncated pointer (size 2)")
		}
		ptrVal = ((ptrBase << 24) | (int(data[offset]) << 16) | (int(data[offset+1]) << 8) | int(data[offset+2])) + 526336
		offset += 3
	case 3:
		if offset+3 >= len(data) {
			return nil, offset, errors.New("mmdb: truncated pointer (size 3)")
		}
		ptrVal = int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	if ptrVal >= len(dataSection) {
		return nil, offset, fmt.Errorf("mmdb: pointer %d out of bounds (section len %d)", ptrVal, len(dataSection))
	}
	val, _, err := decodeRec(dataSection, ptrVal, dataSection, depth+1)
	return val, offset, err // return post-pointer offset, not the pointed-to position
}

// readSize decodes the variable-length size field that follows a control byte.
// low5 is the lower 5 bits of the control byte.
func readSize(data []byte, low5 byte, offset int) (uint32, int, error) {
	switch {
	case low5 < 29:
		return uint32(low5), offset, nil
	case low5 == 29:
		if offset >= len(data) {
			return 0, offset, errors.New("mmdb: truncated size (29)")
		}
		return 29 + uint32(data[offset]), offset + 1, nil
	case low5 == 30:
		if offset+1 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated size (30)")
		}
		size := 285 + (uint32(data[offset])<<8 | uint32(data[offset+1]))
		return size, offset + 2, nil
	default: // 31
		if offset+2 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated size (31)")
		}
		size := 65821 + (uint32(data[offset])<<16 | uint32(data[offset+1])<<8 | uint32(data[offset+2]))
		return size, offset + 3, nil
	}
}
