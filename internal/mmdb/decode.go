// Package mmdb implements a minimal MaxMind DB reader using only stdlib.
// All functions use concrete types — no interface{} — for Yaegi compatibility.
package mmdb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const maxMMDBDepth = 32

// MaxMind DB type codes (high 3 bits of control byte).
const (
	mmdbTypeExtended = 0
	mmdbTypePointer  = 1
	mmdbTypeString   = 2
	mmdbTypeDouble   = 3
	mmdbTypeBytes    = 4
	mmdbTypeUint16   = 5
	mmdbTypeUint32   = 6
	mmdbTypeMap      = 7
	// Extended types: second control byte value + 7.
	mmdbTypeInt32   = 8
	mmdbTypeUint64  = 9
	mmdbTypeUint128 = 10
	mmdbTypeArray   = 11
	mmdbTypeCache   = 12
	mmdbTypeEnd     = 13
	mmdbTypeBool    = 14
	mmdbTypeFloat   = 15
)

// readCtrl reads the type code and low 5 bits from the control byte at offset,
// handling the extended-type second byte. Returns typeCode, low5, next offset.
func readCtrl(data []byte, offset int) (typeCode int, low5 byte, next int, err error) {
	if offset >= len(data) {
		return 0, 0, offset, fmt.Errorf("mmdb: read past end at offset %d", offset)
	}
	ctrl := data[offset]
	next = offset + 1
	typeCode = int(ctrl >> 5)
	low5 = ctrl & 0x1f
	if typeCode == mmdbTypeExtended {
		if next >= len(data) {
			return 0, 0, next, errors.New("mmdb: truncated extended type byte")
		}
		typeCode = int(data[next]) + 7
		next++
	}
	return typeCode, low5, next, nil
}

// readSize decodes the variable-length payload size that follows a control byte.
func readSize(data []byte, low5 byte, offset int) (size int, next int, err error) {
	if low5 < 29 {
		return int(low5), offset, nil
	}
	if low5 == 29 {
		if offset >= len(data) {
			return 0, offset, errors.New("mmdb: truncated size (29)")
		}
		return 29 + int(data[offset]), offset + 1, nil
	}
	if low5 == 30 {
		if offset+1 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated size (30)")
		}
		s := 285 + (int(data[offset])<<8 | int(data[offset+1]))
		return s, offset + 2, nil
	}
	// low5 == 31
	if offset+2 >= len(data) {
		return 0, offset, errors.New("mmdb: truncated size (31)")
	}
	s := 65821 + (int(data[offset])<<16 | int(data[offset+1])<<8 | int(data[offset+2]))
	return s, offset + 3, nil
}

// ptrTarget resolves a pointer and returns its absolute target offset within section,
// plus the offset after the pointer bytes in data.
func ptrTarget(data []byte, low5 byte, offset int) (target int, next int, err error) {
	ptrSize := int((low5 >> 3) & 0x3)
	ptrBase := int(low5 & 0x7)
	switch ptrSize {
	case 0:
		if offset >= len(data) {
			return 0, offset, errors.New("mmdb: truncated pointer (0)")
		}
		return (ptrBase << 8) | int(data[offset]), offset + 1, nil
	case 1:
		if offset+1 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated pointer (1)")
		}
		v := ((ptrBase << 16) | (int(data[offset]) << 8) | int(data[offset+1])) + 2048
		return v, offset + 2, nil
	case 2:
		if offset+2 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated pointer (2)")
		}
		v := ((ptrBase << 24) | (int(data[offset]) << 16) | (int(data[offset+1]) << 8) | int(data[offset+2])) + 526336
		return v, offset + 3, nil
	default: // 3
		if offset+3 >= len(data) {
			return 0, offset, errors.New("mmdb: truncated pointer (3)")
		}
		v := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		return v, offset + 4, nil
	}
}

// skipValue advances offset past a single encoded value without decoding it.
// Pointers are skipped by advancing past their bytes only (not followed).
func skipValue(data []byte, offset int, depth int) (int, error) {
	if depth > maxMMDBDepth {
		return offset, errors.New("mmdb: skip depth limit exceeded")
	}
	typeCode, low5, offset, err := readCtrl(data, offset)
	if err != nil {
		return offset, err
	}
	if typeCode == mmdbTypePointer {
		ptrSize := int((low5 >> 3) & 0x3)
		return offset + ptrSize + 1, nil
	}
	size, offset, err := readSize(data, low5, offset)
	if err != nil {
		return offset, err
	}
	switch typeCode {
	case mmdbTypeString, mmdbTypeBytes:
		return offset + size, nil
	case mmdbTypeUint16, mmdbTypeUint32, mmdbTypeInt32, mmdbTypeUint64, mmdbTypeUint128:
		return offset + size, nil
	case mmdbTypeDouble:
		return offset + 8, nil
	case mmdbTypeFloat:
		return offset + 4, nil
	case mmdbTypeBool, mmdbTypeEnd, mmdbTypeCache:
		return offset, nil
	case mmdbTypeMap:
		for i := 0; i < size; i++ {
			offset, err = skipValue(data, offset, depth+1) // key
			if err != nil {
				return offset, err
			}
			offset, err = skipValue(data, offset, depth+1) // value
			if err != nil {
				return offset, err
			}
		}
		return offset, nil
	case mmdbTypeArray:
		for i := 0; i < size; i++ {
			offset, err = skipValue(data, offset, depth+1)
			if err != nil {
				return offset, err
			}
		}
		return offset, nil
	default:
		return offset, fmt.Errorf("mmdb: unknown type %d in skipValue", typeCode)
	}
}

// readString decodes the string at data[offset], following pointers into section.
// Returns the string and the offset after the value/pointer bytes in data.
func readString(data []byte, offset int, section []byte, depth int) (string, int, error) {
	if depth > maxMMDBDepth {
		return "", offset, errors.New("mmdb: readString depth limit exceeded")
	}
	typeCode, low5, offset, err := readCtrl(data, offset)
	if err != nil {
		return "", offset, err
	}
	if typeCode == mmdbTypePointer {
		target, next, err := ptrTarget(data, low5, offset)
		if err != nil {
			return "", next, err
		}
		if target >= len(section) {
			return "", next, fmt.Errorf("mmdb: string pointer %d out of bounds (len %d)", target, len(section))
		}
		s, _, err := readString(section, target, section, depth+1)
		return s, next, err // advance past pointer bytes in data, not in section
	}
	if typeCode != mmdbTypeString {
		return "", offset, fmt.Errorf("mmdb: expected string type, got %d", typeCode)
	}
	size, offset, err := readSize(data, low5, offset)
	if err != nil {
		return "", offset, err
	}
	end := offset + size
	if end > len(data) {
		return "", offset, errors.New("mmdb: string payload out of bounds")
	}
	return string(data[offset:end]), end, nil
}

// readUint32 decodes the uint value at data[offset], following pointers into section.
func readUint32(data []byte, offset int, section []byte, depth int) (uint32, int, error) {
	if depth > maxMMDBDepth {
		return 0, offset, errors.New("mmdb: readUint32 depth limit exceeded")
	}
	typeCode, low5, offset, err := readCtrl(data, offset)
	if err != nil {
		return 0, offset, err
	}
	if typeCode == mmdbTypePointer {
		target, next, err := ptrTarget(data, low5, offset)
		if err != nil {
			return 0, next, err
		}
		if target >= len(section) {
			return 0, next, fmt.Errorf("mmdb: uint32 pointer %d out of bounds", target)
		}
		v, _, err := readUint32(section, target, section, depth+1)
		return v, next, err
	}
	if typeCode != mmdbTypeUint16 && typeCode != mmdbTypeUint32 {
		return 0, offset, fmt.Errorf("mmdb: expected uint type, got %d", typeCode)
	}
	size, offset, err := readSize(data, low5, offset)
	if err != nil {
		return 0, offset, err
	}
	if size > 4 {
		return 0, offset, fmt.Errorf("mmdb: uint size %d exceeds 4 bytes", size)
	}
	end := offset + size
	if end > len(data) {
		return 0, offset, errors.New("mmdb: uint payload out of bounds")
	}
	var v uint32
	for i := offset; i < end; i++ {
		v = (v << 8) | uint32(data[i])
	}
	return v, end, nil
}
