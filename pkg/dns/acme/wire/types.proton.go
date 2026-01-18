package wire

import (
	"reflect"

	"github.com/outofforest/proton"
	"github.com/outofforest/proton/helpers"
	"github.com/pkg/errors"
)

const (
	id1 uint64 = iota + 1
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
		return id1, nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Size computes the size of marshalled message.
func (m Marshaller) Size(msg any) (uint64, error) {
	switch msg2 := msg.(type) {
	case *MsgRequest:
		return size1(msg2), nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Marshal marshals message.
func (m Marshaller) Marshal(msg any, buf []byte) (retID, retSize uint64, retErr error) {
	defer helpers.RecoverMarshal(&retErr)

	switch msg2 := msg.(type) {
	case *MsgRequest:
		return id1, marshal1(msg2, buf), nil
	default:
		return 0, 0, errors.Errorf("unknown message type %T", msg)
	}
}

// Unmarshal unmarshals message.
func (m Marshaller) Unmarshal(id uint64, buf []byte) (retMsg any, retSize uint64, retErr error) {
	defer helpers.RecoverUnmarshal(&retErr)

	switch id {
	case id1:
		msg := &MsgRequest{}
		return msg, unmarshal1(msg, buf), nil
	default:
		return nil, 0, errors.Errorf("unknown ID %d", id)
	}
}

// IsPatchNeeded checks if non-empty patch exists.
func (m Marshaller) IsPatchNeeded(msgDst, msgSrc any) (bool, error) {
	switch msg2 := msgDst.(type) {
	case *MsgRequest:
		return isPatchNeeded1(msg2, msgSrc.(*MsgRequest)), nil
	default:
		return false, errors.Errorf("unknown message type %T", msgDst)
	}
}

// MakePatch creates a patch.
func (m Marshaller) MakePatch(msgDst, msgSrc any, buf []byte) (retID, retSize uint64, retErr error) {
	defer helpers.RecoverMakePatch(&retErr)

	switch msg2 := msgDst.(type) {
	case *MsgRequest:
		return id1, makePatch1(msg2, msgSrc.(*MsgRequest), buf), nil
	default:
		return 0, 0, errors.Errorf("unknown message type %T", msgDst)
	}
}

// ApplyPatch applies patch.
func (m Marshaller) ApplyPatch(msg any, buf []byte) (retSize uint64, retErr error) {
	defer helpers.RecoverApplyPatch(&retErr)

	switch msg2 := msg.(type) {
	case *MsgRequest:
		return applyPatch1(msg2, buf), nil
	default:
		return 0, errors.Errorf("unknown message type %T", msg)
	}
}

func size1(m *MsgRequest) uint64 {
	var n uint64 = 3
	{
		// Provider

		{
			l := uint64(len(m.Provider))
			helpers.UInt64Size(l, &n)
			n += l
		}
	}
	{
		// AccountURI

		{
			l := uint64(len(m.AccountURI))
			helpers.UInt64Size(l, &n)
			n += l
		}
	}
	{
		// Challenges

		l := uint64(len(m.Challenges))
		helpers.UInt64Size(l, &n)
		for _, sv1 := range m.Challenges {
			n += size0(&sv1)
		}
	}
	return n
}

func marshal1(m *MsgRequest, b []byte) uint64 {
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
		// AccountURI

		{
			l := uint64(len(m.AccountURI))
			helpers.UInt64Marshal(l, b, &o)
			copy(b[o:o+l], m.AccountURI)
			o += l
		}
	}
	{
		// Challenges

		helpers.UInt64Marshal(uint64(len(m.Challenges)), b, &o)
		for _, sv1 := range m.Challenges {
			o += marshal0(&sv1, b[o:])
		}
	}

	return o
}

func unmarshal1(m *MsgRequest, b []byte) uint64 {
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
		// AccountURI

		{
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.AccountURI = string(b[o:o+l])
				o += l
			}
		}
	}
	{
		// Challenges

		var l uint64
		helpers.UInt64Unmarshal(&l, b, &o)
		if l > 0 {
			m.Challenges = make([]Challenge, l)
			for i1 := range l {
				o += unmarshal0(&m.Challenges[i1], b[o:])
			}
		}
	}

	return o
}

func isPatchNeeded1(m, mSrc *MsgRequest) bool {
	{
		// Provider

		if !reflect.DeepEqual(m.Provider, mSrc.Provider) {
			return true
		}

	}
	{
		// AccountURI

		if !reflect.DeepEqual(m.AccountURI, mSrc.AccountURI) {
			return true
		}

	}
	{
		// Challenges

		if !reflect.DeepEqual(m.Challenges, mSrc.Challenges) {
			return true
		}

	}

	return false
}

func makePatch1(m, mSrc *MsgRequest, b []byte) uint64 {
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
		// AccountURI

		if reflect.DeepEqual(m.AccountURI, mSrc.AccountURI) {
			b[0] &= 0xFD
		} else {
			b[0] |= 0x02
			{
				l := uint64(len(m.AccountURI))
				helpers.UInt64Marshal(l, b, &o)
				copy(b[o:o+l], m.AccountURI)
				o += l
			}
		}
	}
	{
		// Challenges

		if reflect.DeepEqual(m.Challenges, mSrc.Challenges) {
			b[0] &= 0xFB
		} else {
			b[0] |= 0x04
			helpers.UInt64Marshal(uint64(len(m.Challenges)), b, &o)
			for _, sv1 := range m.Challenges {
				o += marshal0(&sv1, b[o:])
			}
		}
	}

	return o
}

func applyPatch1(m *MsgRequest, b []byte) uint64 {
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
		// AccountURI

		if b[0]&0x02 != 0 {
			{
				var l uint64
				helpers.UInt64Unmarshal(&l, b, &o)
				if l > 0 {
					m.AccountURI = string(b[o:o+l])
					o += l
				}
			}
		}
	}
	{
		// Challenges

		if b[0]&0x04 != 0 {
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.Challenges = make([]Challenge, l)
				for i1 := range l {
					o += unmarshal0(&m.Challenges[i1], b[o:])
				}
			}
		}
	}

	return o
}

func size0(m *Challenge) uint64 {
	var n uint64 = 2
	{
		// Domain

		{
			l := uint64(len(m.Domain))
			helpers.UInt64Size(l, &n)
			n += l
		}
	}
	{
		// Value

		{
			l := uint64(len(m.Value))
			helpers.UInt64Size(l, &n)
			n += l
		}
	}
	return n
}

func marshal0(m *Challenge, b []byte) uint64 {
	var o uint64
	{
		// Domain

		{
			l := uint64(len(m.Domain))
			helpers.UInt64Marshal(l, b, &o)
			copy(b[o:o+l], m.Domain)
			o += l
		}
	}
	{
		// Value

		{
			l := uint64(len(m.Value))
			helpers.UInt64Marshal(l, b, &o)
			copy(b[o:o+l], m.Value)
			o += l
		}
	}

	return o
}

func unmarshal0(m *Challenge, b []byte) uint64 {
	var o uint64
	{
		// Domain

		{
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.Domain = string(b[o:o+l])
				o += l
			}
		}
	}
	{
		// Value

		{
			var l uint64
			helpers.UInt64Unmarshal(&l, b, &o)
			if l > 0 {
				m.Value = string(b[o:o+l])
				o += l
			}
		}
	}

	return o
}
