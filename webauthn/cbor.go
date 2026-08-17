package webauthn

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// A CBOR reader covering exactly the subset WebAuthn uses: the attestation
// object map, and the COSE key inside it. Unsigned and negative integers, byte
// and text strings, arrays and maps — no tags, no floats, no indefinite
// lengths, no streaming.
//
// Hand-rolled rather than pulled in because the subset is this small and this
// code sits on the authentication path: a dependency here would be a much
// larger surface to audit than the eighty lines below.

var errCBOR = errors.New("malformed CBOR")

type cborReader struct {
	buf []byte
	pos int
}

const (
	majUint   = 0
	majNegInt = 1
	majBytes  = 2
	majText   = 3
	majArray  = 4
	majMap    = 5
)

// head reads one initial byte plus any following length bytes, returning the
// major type and the argument.
func (r *cborReader) head() (major byte, arg uint64, err error) {
	if r.pos >= len(r.buf) {
		return 0, 0, errCBOR
	}
	b := r.buf[r.pos]
	r.pos++
	major = b >> 5
	switch info := b & 0x1f; {
	case info < 24:
		arg = uint64(info)
	case info == 24:
		if r.pos+1 > len(r.buf) {
			return 0, 0, errCBOR
		}
		arg = uint64(r.buf[r.pos])
		r.pos++
	case info == 25:
		if r.pos+2 > len(r.buf) {
			return 0, 0, errCBOR
		}
		arg = uint64(binary.BigEndian.Uint16(r.buf[r.pos:]))
		r.pos += 2
	case info == 26:
		if r.pos+4 > len(r.buf) {
			return 0, 0, errCBOR
		}
		arg = uint64(binary.BigEndian.Uint32(r.buf[r.pos:]))
		r.pos += 4
	case info == 27:
		if r.pos+8 > len(r.buf) {
			return 0, 0, errCBOR
		}
		arg = binary.BigEndian.Uint64(r.buf[r.pos:])
		r.pos += 8
	default:
		return 0, 0, fmt.Errorf("%w: unsupported additional information %d", errCBOR, info)
	}
	return major, arg, nil
}

// value reads one complete item. Integers come back as int64, strings as
// string, byte strings as []byte, arrays as []any, maps as map[any]any.
func (r *cborReader) value() (any, error) {
	major, arg, err := r.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case majUint:
		return int64(arg), nil
	case majNegInt:
		return -1 - int64(arg), nil
	case majBytes, majText:
		if arg > uint64(len(r.buf)-r.pos) {
			return nil, errCBOR
		}
		raw := r.buf[r.pos : r.pos+int(arg)]
		r.pos += int(arg)
		if major == majText {
			return string(raw), nil
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	case majArray:
		items := make([]any, 0, min(int(arg), 64))
		for i := uint64(0); i < arg; i++ {
			v, err := r.value()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	case majMap:
		m := make(map[any]any, min(int(arg), 64))
		for i := uint64(0); i < arg; i++ {
			k, err := r.value()
			if err != nil {
				return nil, err
			}
			v, err := r.value()
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil
	}
	return nil, fmt.Errorf("%w: unsupported major type %d", errCBOR, major)
}

// cborMap decodes a byte slice expected to hold a single CBOR map.
func cborMap(b []byte) (map[any]any, error) {
	r := &cborReader{buf: b}
	v, err := r.value()
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected a map", errCBOR)
	}
	return m, nil
}

func mapBytes(m map[any]any, key any) ([]byte, bool) {
	b, ok := m[key].([]byte)
	return b, ok
}

func mapInt(m map[any]any, key any) (int64, bool) {
	i, ok := m[key].(int64)
	return i, ok
}
