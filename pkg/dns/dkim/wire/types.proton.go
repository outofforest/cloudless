package wire

import (
	"reflect"
	"unsafe"

	"github.com/outofforest/proton"
	"github.com/outofforest/proton/helpers"
	"github.com/pkg/errors"
)

const (
	id0 uint64 = iota + 1
)

var _ proton.Marshaller = Marshaller{}

// NewMarshaller creates marshaller.
func NewMarshaller() Marshaller {
	return Marshaller{}
}

// Marshaller marshals and unmarshals messages.
type Marshaller struct {
}

// Messages returns list of the message types supported by marshaller.
func (m Marshaller) Messages() []any {
	return []any {
		MsgRequest{},
	}
}

// ID returns ID of message type.
func (m Marshaller) ID(msg any) (uint64, error) {
	switch msg.(type) {
	case *MsgRequest:
		return id0, nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Size computes the size of marshalled message.
func (m Marshaller) Size(msg any) (uint64, error) {
	switch msg2 := msg.(type) {
	case *MsgRequest:
		return size0(msg2), nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Marshal marshals message.
func (m Marshaller) Marshal(msg any, buf []byte) (retID, retSize uint64, retErr error) {
	defer helpers.RecoverMarshal(&retErr)

	switch msg2 := msg.(type) {
	case *MsgRequest:
		return id0, marshal0(msg2, buf), nil
	default:
		return 0, 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Unmarshal unmarshals message.
func (m Marshaller) Unmarshal(id uint64, buf []byte) (retMsg any, retSize uint64, retErr error) {
	defer helpers.RecoverUnmarshal(&retErr)

	switch id {
	case id0:
		msg := &MsgRequest{}
		return msg, unmarshal0(msg, buf), nil
	default:
		return nil, 0, errors.Errorf("unknown ID %d", id)
	}
}

// IsPatchNeeded checks if non-empty patch exists.
func (m Marshaller) IsPatchNeeded(msgDst, msgSrc any) (bool, error) {
	switch msg2 := msgDst.(type) {
	case *MsgRequest:
		return isPatchNeeded0(msg2, msgSrc.(*MsgRequest)), nil
	default:
		return false, errors.Errorf("unknown message type %T", msgDst)
	}
}

// MakePatch creates a patch.
func (m Marshaller) MakePatch(msgDst, msgSrc any, buf []byte) (retID, retSize uint64, retErr error) {
	defer helpers.RecoverMakePatch(&retErr)

	switch msg2 := msgDst.(type) {
	case *MsgRequest:
		return id0, makePatch0(msg2, msgSrc.(*MsgRequest), buf), nil
	default:
		return 0, 0, errors.Errorf("unknown message type %T", msgDst)
	}
}

// ApplyPatch applies patch.
func (m Marshaller) ApplyPatch(msg any, buf []byte) (retSize uint64, retErr error) {
	defer helpers.RecoverApplyPatch(&retErr)

	switch msg2 := msg.(type) {
	case *MsgRequest:
		return applyPatch0(msg2, buf), nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

func size0(m *MsgRequest) uint64 {
	var n uint64 = 2
	{
		// Provider

		{
			l := uint64(len(m.Provider))
			helpers.UInt64Size(l, &n)
			n += l
		}
	}
	{
		// PublicKey

		l := uint64(len(m.PublicKey))
		helpers.UInt64Size(l, &n)
		n += l
	}
	return n
}

func marshal0(m *MsgRequest, b []byte) uint64 {
	var o uint64
	{
		// Provider

		{
			l := uint64(len(m.Provider))
			helpers.UInt64Marshal(l, b, &o)
			copy(b[o:o+l], m.Provider)
			o += l
		}
	}
	{
		// PublicKey

		l := uint64(len(m.PublicKey))
		helpers.UInt64Marshal(l, b, &o)
		if l > 0 {
			copy(b[o:o+l], unsafe.Slice(&m.PublicKey[0], l))
			o += l
		}
	}

	return o
}

func unmarshal0(m *MsgRequest, b []byte) uint64 {
	var o uint64
	{
		// Provider

		{
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.Provider = string(b[o:o+l])
				o += l
			}
		}
	}
	{
		// PublicKey

		var l uint64
		helpers.UInt64Unmarshal(&l, b, &o)
		if l > 0 {
			m.PublicKey = make([]uint8, l)
			copy(m.PublicKey, b[o:o+l])
			o += l
		}
	}

	return o
}

func isPatchNeeded0(m, mSrc *MsgRequest) bool {
	{
		// Provider

		if !reflect.DeepEqual(m.Provider, mSrc.Provider) {
			return true
		}

	}
	{
		// PublicKey

		if !reflect.DeepEqual(m.PublicKey, mSrc.PublicKey) {
			return true
		}

	}

	return false
}

func makePatch0(m, mSrc *MsgRequest, b []byte) uint64 {
	var o uint64 = 1
	{
		// Provider

		if reflect.DeepEqual(m.Provider, mSrc.Provider) {
			b[0] &= 0xFE
		} else {
			b[0] |= 0x01
			{
				l := uint64(len(m.Provider))
				helpers.UInt64Marshal(l, b, &o)
				copy(b[o:o+l], m.Provider)
				o += l
			}
		}
	}
	{
		// PublicKey

		if reflect.DeepEqual(m.PublicKey, mSrc.PublicKey) {
			b[0] &= 0xFD
		} else {
			b[0] |= 0x02
			l := uint64(len(m.PublicKey))
			helpers.UInt64Marshal(l, b, &o)
			if l > 0 {
				copy(b[o:o+l], unsafe.Slice(&m.PublicKey[0], l))
				o += l
			}
		}
	}

	return o
}

func applyPatch0(m *MsgRequest, b []byte) uint64 {
	var o uint64 = 1
	{
		// Provider

		if b[0]&0x01 != 0 {
			{
				var l uint64
				helpers.UInt64Unmarshal(&l, b, &o)
				if l > 0 {
					m.Provider = string(b[o:o+l])
					o += l
				}
			}
		}
	}
	{
		// PublicKey

		if b[0]&0x02 != 0 {
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.PublicKey = make([]uint8, l)
				copy(m.PublicKey, b[o:o+l])
				o += l
			}
		}
	}

	return o
}
