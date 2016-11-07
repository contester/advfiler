// Code generated by protoc-gen-gogo.
// source: git.stingr.net/stingray/advfiler/protos/protos.proto
// DO NOT EDIT!

/*
	Package protos is a generated protocol buffer package.

	It is generated from these files:
		git.stingr.net/stingray/advfiler/protos/protos.proto

	It has these top-level messages:
		FileChunk
		FileInfo
*/
package protos

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "github.com/gogo/protobuf/gogoproto"

import bytes "bytes"

import strings "strings"
import reflect "reflect"

import io "io"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
const _ = proto.ProtoPackageIsVersion1

type FileChunk struct {
	Fid     string `protobuf:"bytes,1,opt,name=fid,proto3" json:"fid,omitempty"`
	Sha1Sum []byte `protobuf:"bytes,2,opt,name=sha1sum,proto3" json:"sha1sum,omitempty"`
	Size_   int64  `protobuf:"varint,3,opt,name=size,proto3" json:"size,omitempty"`
}

func (m *FileChunk) Reset()                    { *m = FileChunk{} }
func (*FileChunk) ProtoMessage()               {}
func (*FileChunk) Descriptor() ([]byte, []int) { return fileDescriptorProtos, []int{0} }

type FileInfo struct {
	Size_      int64             `protobuf:"varint,1,opt,name=size,proto3" json:"size,omitempty"`
	Digests    *FileInfo_Digests `protobuf:"bytes,2,opt,name=digests" json:"digests,omitempty"`
	ModuleType string            `protobuf:"bytes,3,opt,name=module_type,json=moduleType,proto3" json:"module_type,omitempty"`
	Chunks     []*FileChunk      `protobuf:"bytes,4,rep,name=chunks" json:"chunks,omitempty"`
}

func (m *FileInfo) Reset()                    { *m = FileInfo{} }
func (*FileInfo) ProtoMessage()               {}
func (*FileInfo) Descriptor() ([]byte, []int) { return fileDescriptorProtos, []int{1} }

func (m *FileInfo) GetDigests() *FileInfo_Digests {
	if m != nil {
		return m.Digests
	}
	return nil
}

func (m *FileInfo) GetChunks() []*FileChunk {
	if m != nil {
		return m.Chunks
	}
	return nil
}

type FileInfo_Digests struct {
	Sha1 []byte `protobuf:"bytes,1,opt,name=sha1,proto3" json:"sha1,omitempty"`
	Md5  []byte `protobuf:"bytes,2,opt,name=md5,proto3" json:"md5,omitempty"`
}

func (m *FileInfo_Digests) Reset()                    { *m = FileInfo_Digests{} }
func (*FileInfo_Digests) ProtoMessage()               {}
func (*FileInfo_Digests) Descriptor() ([]byte, []int) { return fileDescriptorProtos, []int{1, 0} }

func init() {
	proto.RegisterType((*FileChunk)(nil), "protos.FileChunk")
	proto.RegisterType((*FileInfo)(nil), "protos.FileInfo")
	proto.RegisterType((*FileInfo_Digests)(nil), "protos.FileInfo.Digests")
}
func (this *FileChunk) Equal(that interface{}) bool {
	if that == nil {
		if this == nil {
			return true
		}
		return false
	}

	that1, ok := that.(*FileChunk)
	if !ok {
		that2, ok := that.(FileChunk)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		if this == nil {
			return true
		}
		return false
	} else if this == nil {
		return false
	}
	if this.Fid != that1.Fid {
		return false
	}
	if !bytes.Equal(this.Sha1Sum, that1.Sha1Sum) {
		return false
	}
	if this.Size_ != that1.Size_ {
		return false
	}
	return true
}
func (this *FileInfo) Equal(that interface{}) bool {
	if that == nil {
		if this == nil {
			return true
		}
		return false
	}

	that1, ok := that.(*FileInfo)
	if !ok {
		that2, ok := that.(FileInfo)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		if this == nil {
			return true
		}
		return false
	} else if this == nil {
		return false
	}
	if this.Size_ != that1.Size_ {
		return false
	}
	if !this.Digests.Equal(that1.Digests) {
		return false
	}
	if this.ModuleType != that1.ModuleType {
		return false
	}
	if len(this.Chunks) != len(that1.Chunks) {
		return false
	}
	for i := range this.Chunks {
		if !this.Chunks[i].Equal(that1.Chunks[i]) {
			return false
		}
	}
	return true
}
func (this *FileInfo_Digests) Equal(that interface{}) bool {
	if that == nil {
		if this == nil {
			return true
		}
		return false
	}

	that1, ok := that.(*FileInfo_Digests)
	if !ok {
		that2, ok := that.(FileInfo_Digests)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		if this == nil {
			return true
		}
		return false
	} else if this == nil {
		return false
	}
	if !bytes.Equal(this.Sha1, that1.Sha1) {
		return false
	}
	if !bytes.Equal(this.Md5, that1.Md5) {
		return false
	}
	return true
}
func (m *FileChunk) Marshal() (data []byte, err error) {
	size := m.Size()
	data = make([]byte, size)
	n, err := m.MarshalTo(data)
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}

func (m *FileChunk) MarshalTo(data []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Fid) > 0 {
		data[i] = 0xa
		i++
		i = encodeVarintProtos(data, i, uint64(len(m.Fid)))
		i += copy(data[i:], m.Fid)
	}
	if len(m.Sha1Sum) > 0 {
		data[i] = 0x12
		i++
		i = encodeVarintProtos(data, i, uint64(len(m.Sha1Sum)))
		i += copy(data[i:], m.Sha1Sum)
	}
	if m.Size_ != 0 {
		data[i] = 0x18
		i++
		i = encodeVarintProtos(data, i, uint64(m.Size_))
	}
	return i, nil
}

func (m *FileInfo) Marshal() (data []byte, err error) {
	size := m.Size()
	data = make([]byte, size)
	n, err := m.MarshalTo(data)
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}

func (m *FileInfo) MarshalTo(data []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if m.Size_ != 0 {
		data[i] = 0x8
		i++
		i = encodeVarintProtos(data, i, uint64(m.Size_))
	}
	if m.Digests != nil {
		data[i] = 0x12
		i++
		i = encodeVarintProtos(data, i, uint64(m.Digests.Size()))
		n1, err := m.Digests.MarshalTo(data[i:])
		if err != nil {
			return 0, err
		}
		i += n1
	}
	if len(m.ModuleType) > 0 {
		data[i] = 0x1a
		i++
		i = encodeVarintProtos(data, i, uint64(len(m.ModuleType)))
		i += copy(data[i:], m.ModuleType)
	}
	if len(m.Chunks) > 0 {
		for _, msg := range m.Chunks {
			data[i] = 0x22
			i++
			i = encodeVarintProtos(data, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(data[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	return i, nil
}

func (m *FileInfo_Digests) Marshal() (data []byte, err error) {
	size := m.Size()
	data = make([]byte, size)
	n, err := m.MarshalTo(data)
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}

func (m *FileInfo_Digests) MarshalTo(data []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Sha1) > 0 {
		data[i] = 0xa
		i++
		i = encodeVarintProtos(data, i, uint64(len(m.Sha1)))
		i += copy(data[i:], m.Sha1)
	}
	if len(m.Md5) > 0 {
		data[i] = 0x12
		i++
		i = encodeVarintProtos(data, i, uint64(len(m.Md5)))
		i += copy(data[i:], m.Md5)
	}
	return i, nil
}

func encodeFixed64Protos(data []byte, offset int, v uint64) int {
	data[offset] = uint8(v)
	data[offset+1] = uint8(v >> 8)
	data[offset+2] = uint8(v >> 16)
	data[offset+3] = uint8(v >> 24)
	data[offset+4] = uint8(v >> 32)
	data[offset+5] = uint8(v >> 40)
	data[offset+6] = uint8(v >> 48)
	data[offset+7] = uint8(v >> 56)
	return offset + 8
}
func encodeFixed32Protos(data []byte, offset int, v uint32) int {
	data[offset] = uint8(v)
	data[offset+1] = uint8(v >> 8)
	data[offset+2] = uint8(v >> 16)
	data[offset+3] = uint8(v >> 24)
	return offset + 4
}
func encodeVarintProtos(data []byte, offset int, v uint64) int {
	for v >= 1<<7 {
		data[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	data[offset] = uint8(v)
	return offset + 1
}
func (m *FileChunk) Size() (n int) {
	var l int
	_ = l
	l = len(m.Fid)
	if l > 0 {
		n += 1 + l + sovProtos(uint64(l))
	}
	l = len(m.Sha1Sum)
	if l > 0 {
		n += 1 + l + sovProtos(uint64(l))
	}
	if m.Size_ != 0 {
		n += 1 + sovProtos(uint64(m.Size_))
	}
	return n
}

func (m *FileInfo) Size() (n int) {
	var l int
	_ = l
	if m.Size_ != 0 {
		n += 1 + sovProtos(uint64(m.Size_))
	}
	if m.Digests != nil {
		l = m.Digests.Size()
		n += 1 + l + sovProtos(uint64(l))
	}
	l = len(m.ModuleType)
	if l > 0 {
		n += 1 + l + sovProtos(uint64(l))
	}
	if len(m.Chunks) > 0 {
		for _, e := range m.Chunks {
			l = e.Size()
			n += 1 + l + sovProtos(uint64(l))
		}
	}
	return n
}

func (m *FileInfo_Digests) Size() (n int) {
	var l int
	_ = l
	l = len(m.Sha1)
	if l > 0 {
		n += 1 + l + sovProtos(uint64(l))
	}
	l = len(m.Md5)
	if l > 0 {
		n += 1 + l + sovProtos(uint64(l))
	}
	return n
}

func sovProtos(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}
func sozProtos(x uint64) (n int) {
	return sovProtos(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (this *FileChunk) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&FileChunk{`,
		`Fid:` + fmt.Sprintf("%v", this.Fid) + `,`,
		`Sha1Sum:` + fmt.Sprintf("%v", this.Sha1Sum) + `,`,
		`Size_:` + fmt.Sprintf("%v", this.Size_) + `,`,
		`}`,
	}, "")
	return s
}
func (this *FileInfo) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&FileInfo{`,
		`Size_:` + fmt.Sprintf("%v", this.Size_) + `,`,
		`Digests:` + strings.Replace(fmt.Sprintf("%v", this.Digests), "FileInfo_Digests", "FileInfo_Digests", 1) + `,`,
		`ModuleType:` + fmt.Sprintf("%v", this.ModuleType) + `,`,
		`Chunks:` + strings.Replace(fmt.Sprintf("%v", this.Chunks), "FileChunk", "FileChunk", 1) + `,`,
		`}`,
	}, "")
	return s
}
func (this *FileInfo_Digests) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&FileInfo_Digests{`,
		`Sha1:` + fmt.Sprintf("%v", this.Sha1) + `,`,
		`Md5:` + fmt.Sprintf("%v", this.Md5) + `,`,
		`}`,
	}, "")
	return s
}
func valueToStringProtos(v interface{}) string {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return "nil"
	}
	pv := reflect.Indirect(rv).Interface()
	return fmt.Sprintf("*%v", pv)
}
func (m *FileChunk) Unmarshal(data []byte) error {
	l := len(data)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProtos
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := data[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: FileChunk: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: FileChunk: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Fid", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				stringLen |= (uint64(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + intStringLen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Fid = string(data[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Sha1Sum", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				byteLen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + byteLen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Sha1Sum = append(m.Sha1Sum[:0], data[iNdEx:postIndex]...)
			if m.Sha1Sum == nil {
				m.Sha1Sum = []byte{}
			}
			iNdEx = postIndex
		case 3:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Size_", wireType)
			}
			m.Size_ = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				m.Size_ |= (int64(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(data[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthProtos
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *FileInfo) Unmarshal(data []byte) error {
	l := len(data)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProtos
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := data[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: FileInfo: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: FileInfo: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Size_", wireType)
			}
			m.Size_ = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				m.Size_ |= (int64(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Digests", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			if m.Digests == nil {
				m.Digests = &FileInfo_Digests{}
			}
			if err := m.Digests.Unmarshal(data[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field ModuleType", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				stringLen |= (uint64(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + intStringLen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.ModuleType = string(data[iNdEx:postIndex])
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Chunks", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				msglen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + msglen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Chunks = append(m.Chunks, &FileChunk{})
			if err := m.Chunks[len(m.Chunks)-1].Unmarshal(data[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(data[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthProtos
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *FileInfo_Digests) Unmarshal(data []byte) error {
	l := len(data)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProtos
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := data[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: Digests: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: Digests: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Sha1", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				byteLen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + byteLen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Sha1 = append(m.Sha1[:0], data[iNdEx:postIndex]...)
			if m.Sha1 == nil {
				m.Sha1 = []byte{}
			}
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Md5", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				byteLen |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProtos
			}
			postIndex := iNdEx + byteLen
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Md5 = append(m.Md5[:0], data[iNdEx:postIndex]...)
			if m.Md5 == nil {
				m.Md5 = []byte{}
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(data[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthProtos
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipProtos(data []byte) (n int, err error) {
	l := len(data)
	iNdEx := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowProtos
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := data[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if data[iNdEx-1] < 0x80 {
					break
				}
			}
			return iNdEx, nil
		case 1:
			iNdEx += 8
			return iNdEx, nil
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowProtos
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := data[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			iNdEx += length
			if length < 0 {
				return 0, ErrInvalidLengthProtos
			}
			return iNdEx, nil
		case 3:
			for {
				var innerWire uint64
				var start int = iNdEx
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return 0, ErrIntOverflowProtos
					}
					if iNdEx >= l {
						return 0, io.ErrUnexpectedEOF
					}
					b := data[iNdEx]
					iNdEx++
					innerWire |= (uint64(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				innerWireType := int(innerWire & 0x7)
				if innerWireType == 4 {
					break
				}
				next, err := skipProtos(data[start:])
				if err != nil {
					return 0, err
				}
				iNdEx = start + next
			}
			return iNdEx, nil
		case 4:
			return iNdEx, nil
		case 5:
			iNdEx += 4
			return iNdEx, nil
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
	}
	panic("unreachable")
}

var (
	ErrInvalidLengthProtos = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowProtos   = fmt.Errorf("proto: integer overflow")
)

var fileDescriptorProtos = []byte{
	// 305 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x4c, 0x50, 0x3d, 0x4e, 0xf3, 0x40,
	0x10, 0x8d, 0x3f, 0x47, 0xf1, 0xe7, 0x49, 0x0a, 0xd8, 0x6a, 0x15, 0x21, 0x13, 0xa5, 0x82, 0x82,
	0xb5, 0x08, 0x70, 0x01, 0x40, 0x48, 0x88, 0x6e, 0x45, 0x8f, 0xec, 0xf8, 0x6f, 0x45, 0xec, 0x8d,
	0xbc, 0x6b, 0xa4, 0x50, 0x71, 0x0c, 0x8e, 0xc0, 0x51, 0xe8, 0xa0, 0xa4, 0xe4, 0xe7, 0x22, 0x8c,
	0x77, 0x6d, 0xa0, 0x18, 0xcd, 0x9b, 0xe7, 0x37, 0xcf, 0xf3, 0x16, 0x8e, 0x73, 0xa1, 0x99, 0xd2,
	0xa2, 0xca, 0x6b, 0x56, 0xa5, 0x3a, 0xb4, 0x30, 0xda, 0x84, 0x51, 0x72, 0x97, 0x89, 0x55, 0x5a,
	0x87, 0xeb, 0x5a, 0x6a, 0xa9, 0xba, 0xc6, 0x4c, 0x23, 0x23, 0x3b, 0x4d, 0x0f, 0x70, 0xbb, 0x68,
	0x62, 0xb6, 0x94, 0x65, 0x98, 0xcb, 0x5c, 0x5a, 0x55, 0xdc, 0x64, 0x66, 0x32, 0x83, 0x41, 0x76,
	0x6d, 0x7e, 0x05, 0xfe, 0x05, 0x7a, 0x9e, 0x15, 0x4d, 0x75, 0x4b, 0xb6, 0xc0, 0xcd, 0x44, 0x42,
	0x9d, 0x99, 0xb3, 0xe7, 0xf3, 0x16, 0x12, 0x0a, 0x9e, 0x2a, 0xa2, 0x43, 0xd5, 0x94, 0xf4, 0x1f,
	0xb2, 0x13, 0xde, 0x8f, 0x84, 0xc0, 0x50, 0x89, 0xfb, 0x94, 0xba, 0x48, 0xbb, 0xdc, 0xe0, 0xf9,
	0x8b, 0x03, 0xff, 0x5b, 0xb7, 0xcb, 0x2a, 0x93, 0x3f, 0x02, 0xe7, 0x57, 0x40, 0x16, 0xe0, 0x25,
	0x22, 0x4f, 0x95, 0x56, 0xc6, 0x6e, 0xbc, 0xa0, 0xac, 0x0b, 0xd1, 0xaf, 0xb1, 0x73, 0xfb, 0x9d,
	0xf7, 0x42, 0xb2, 0x0b, 0xe3, 0x52, 0x26, 0xcd, 0x2a, 0xbd, 0xd1, 0x9b, 0xb5, 0xfd, 0x9f, 0xcf,
	0xc1, 0x52, 0xd7, 0xc8, 0x90, 0x7d, 0x18, 0x2d, 0xdb, 0xf3, 0x15, 0x1d, 0xce, 0x5c, 0xf4, 0xdc,
	0xfe, 0xeb, 0x69, 0x82, 0xf1, 0x4e, 0x30, 0x0d, 0xc1, 0xeb, 0xfc, 0xcd, 0x79, 0x18, 0xc5, 0x9c,
	0x37, 0xe1, 0x06, 0xb7, 0xf9, 0xcb, 0xe4, 0xa4, 0x4b, 0xda, 0xc2, 0xd3, 0x9d, 0xb7, 0x8f, 0x60,
	0xf0, 0xf0, 0x19, 0x38, 0x4f, 0x58, 0xcf, 0x58, 0xaf, 0x58, 0xef, 0x58, 0x8f, 0x5f, 0xc1, 0x20,
	0xb6, 0x6f, 0x7e, 0xf4, 0x1d, 0x00, 0x00, 0xff, 0xff, 0x4e, 0xdf, 0xd1, 0xd3, 0xb2, 0x01, 0x00,
	0x00,
}