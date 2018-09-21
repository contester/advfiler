// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: protos.proto

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
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type FileChunk struct {
	Fid     string `protobuf:"bytes,1,opt,name=fid,proto3" json:"fid,omitempty"`
	Sha1Sum []byte `protobuf:"bytes,2,opt,name=sha1sum,proto3" json:"sha1sum,omitempty"`
	Size_   int64  `protobuf:"varint,3,opt,name=size,proto3" json:"size,omitempty"`
}

func (m *FileChunk) Reset()      { *m = FileChunk{} }
func (*FileChunk) ProtoMessage() {}
func (*FileChunk) Descriptor() ([]byte, []int) {
	return fileDescriptor_protos_5d00d5d0475a66b2, []int{0}
}
func (m *FileChunk) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *FileChunk) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_FileChunk.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *FileChunk) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileChunk.Merge(dst, src)
}
func (m *FileChunk) XXX_Size() int {
	return m.Size()
}
func (m *FileChunk) XXX_DiscardUnknown() {
	xxx_messageInfo_FileChunk.DiscardUnknown(m)
}

var xxx_messageInfo_FileChunk proto.InternalMessageInfo

func (m *FileChunk) GetFid() string {
	if m != nil {
		return m.Fid
	}
	return ""
}

func (m *FileChunk) GetSha1Sum() []byte {
	if m != nil {
		return m.Sha1Sum
	}
	return nil
}

func (m *FileChunk) GetSize_() int64 {
	if m != nil {
		return m.Size_
	}
	return 0
}

type FileInfo struct {
	Size_      int64             `protobuf:"varint,1,opt,name=size,proto3" json:"size,omitempty"`
	Digests    *FileInfo_Digests `protobuf:"bytes,2,opt,name=digests" json:"digests,omitempty"`
	ModuleType string            `protobuf:"bytes,3,opt,name=module_type,json=moduleType,proto3" json:"module_type,omitempty"`
	Chunks     []*FileChunk      `protobuf:"bytes,4,rep,name=chunks" json:"chunks,omitempty"`
}

func (m *FileInfo) Reset()      { *m = FileInfo{} }
func (*FileInfo) ProtoMessage() {}
func (*FileInfo) Descriptor() ([]byte, []int) {
	return fileDescriptor_protos_5d00d5d0475a66b2, []int{1}
}
func (m *FileInfo) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *FileInfo) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_FileInfo.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *FileInfo) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileInfo.Merge(dst, src)
}
func (m *FileInfo) XXX_Size() int {
	return m.Size()
}
func (m *FileInfo) XXX_DiscardUnknown() {
	xxx_messageInfo_FileInfo.DiscardUnknown(m)
}

var xxx_messageInfo_FileInfo proto.InternalMessageInfo

func (m *FileInfo) GetSize_() int64 {
	if m != nil {
		return m.Size_
	}
	return 0
}

func (m *FileInfo) GetDigests() *FileInfo_Digests {
	if m != nil {
		return m.Digests
	}
	return nil
}

func (m *FileInfo) GetModuleType() string {
	if m != nil {
		return m.ModuleType
	}
	return ""
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

func (m *FileInfo_Digests) Reset()      { *m = FileInfo_Digests{} }
func (*FileInfo_Digests) ProtoMessage() {}
func (*FileInfo_Digests) Descriptor() ([]byte, []int) {
	return fileDescriptor_protos_5d00d5d0475a66b2, []int{1, 0}
}
func (m *FileInfo_Digests) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *FileInfo_Digests) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_FileInfo_Digests.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (dst *FileInfo_Digests) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileInfo_Digests.Merge(dst, src)
}
func (m *FileInfo_Digests) XXX_Size() int {
	return m.Size()
}
func (m *FileInfo_Digests) XXX_DiscardUnknown() {
	xxx_messageInfo_FileInfo_Digests.DiscardUnknown(m)
}

var xxx_messageInfo_FileInfo_Digests proto.InternalMessageInfo

func (m *FileInfo_Digests) GetSha1() []byte {
	if m != nil {
		return m.Sha1
	}
	return nil
}

func (m *FileInfo_Digests) GetMd5() []byte {
	if m != nil {
		return m.Md5
	}
	return nil
}

func init() {
	proto.RegisterType((*FileChunk)(nil), "protos.FileChunk")
	proto.RegisterType((*FileInfo)(nil), "protos.FileInfo")
	proto.RegisterType((*FileInfo_Digests)(nil), "protos.FileInfo.Digests")
}
func (this *FileChunk) Equal(that interface{}) bool {
	if that == nil {
		return this == nil
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
		return this == nil
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
		return this == nil
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
		return this == nil
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
		return this == nil
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
		return this == nil
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
func (this *FileChunk) GoString() string {
	if this == nil {
		return "nil"
	}
	s := make([]string, 0, 7)
	s = append(s, "&protos.FileChunk{")
	s = append(s, "Fid: "+fmt.Sprintf("%#v", this.Fid)+",\n")
	s = append(s, "Sha1Sum: "+fmt.Sprintf("%#v", this.Sha1Sum)+",\n")
	s = append(s, "Size_: "+fmt.Sprintf("%#v", this.Size_)+",\n")
	s = append(s, "}")
	return strings.Join(s, "")
}
func (this *FileInfo) GoString() string {
	if this == nil {
		return "nil"
	}
	s := make([]string, 0, 8)
	s = append(s, "&protos.FileInfo{")
	s = append(s, "Size_: "+fmt.Sprintf("%#v", this.Size_)+",\n")
	if this.Digests != nil {
		s = append(s, "Digests: "+fmt.Sprintf("%#v", this.Digests)+",\n")
	}
	s = append(s, "ModuleType: "+fmt.Sprintf("%#v", this.ModuleType)+",\n")
	if this.Chunks != nil {
		s = append(s, "Chunks: "+fmt.Sprintf("%#v", this.Chunks)+",\n")
	}
	s = append(s, "}")
	return strings.Join(s, "")
}
func (this *FileInfo_Digests) GoString() string {
	if this == nil {
		return "nil"
	}
	s := make([]string, 0, 6)
	s = append(s, "&protos.FileInfo_Digests{")
	s = append(s, "Sha1: "+fmt.Sprintf("%#v", this.Sha1)+",\n")
	s = append(s, "Md5: "+fmt.Sprintf("%#v", this.Md5)+",\n")
	s = append(s, "}")
	return strings.Join(s, "")
}
func valueToGoStringProtos(v interface{}, typ string) string {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return "nil"
	}
	pv := reflect.Indirect(rv).Interface()
	return fmt.Sprintf("func(v %v) *%v { return &v } ( %#v )", typ, typ, pv)
}
func (m *FileChunk) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *FileChunk) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Fid) > 0 {
		dAtA[i] = 0xa
		i++
		i = encodeVarintProtos(dAtA, i, uint64(len(m.Fid)))
		i += copy(dAtA[i:], m.Fid)
	}
	if len(m.Sha1Sum) > 0 {
		dAtA[i] = 0x12
		i++
		i = encodeVarintProtos(dAtA, i, uint64(len(m.Sha1Sum)))
		i += copy(dAtA[i:], m.Sha1Sum)
	}
	if m.Size_ != 0 {
		dAtA[i] = 0x18
		i++
		i = encodeVarintProtos(dAtA, i, uint64(m.Size_))
	}
	return i, nil
}

func (m *FileInfo) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *FileInfo) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if m.Size_ != 0 {
		dAtA[i] = 0x8
		i++
		i = encodeVarintProtos(dAtA, i, uint64(m.Size_))
	}
	if m.Digests != nil {
		dAtA[i] = 0x12
		i++
		i = encodeVarintProtos(dAtA, i, uint64(m.Digests.Size()))
		n1, err := m.Digests.MarshalTo(dAtA[i:])
		if err != nil {
			return 0, err
		}
		i += n1
	}
	if len(m.ModuleType) > 0 {
		dAtA[i] = 0x1a
		i++
		i = encodeVarintProtos(dAtA, i, uint64(len(m.ModuleType)))
		i += copy(dAtA[i:], m.ModuleType)
	}
	if len(m.Chunks) > 0 {
		for _, msg := range m.Chunks {
			dAtA[i] = 0x22
			i++
			i = encodeVarintProtos(dAtA, i, uint64(msg.Size()))
			n, err := msg.MarshalTo(dAtA[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
	}
	return i, nil
}

func (m *FileInfo_Digests) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *FileInfo_Digests) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.Sha1) > 0 {
		dAtA[i] = 0xa
		i++
		i = encodeVarintProtos(dAtA, i, uint64(len(m.Sha1)))
		i += copy(dAtA[i:], m.Sha1)
	}
	if len(m.Md5) > 0 {
		dAtA[i] = 0x12
		i++
		i = encodeVarintProtos(dAtA, i, uint64(len(m.Md5)))
		i += copy(dAtA[i:], m.Md5)
	}
	return i, nil
}

func encodeVarintProtos(dAtA []byte, offset int, v uint64) int {
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return offset + 1
}
func (m *FileChunk) Size() (n int) {
	if m == nil {
		return 0
	}
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
	if m == nil {
		return 0
	}
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
	if m == nil {
		return 0
	}
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
func (m *FileChunk) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
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
			b := dAtA[iNdEx]
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
				b := dAtA[iNdEx]
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
			m.Fid = string(dAtA[iNdEx:postIndex])
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
				b := dAtA[iNdEx]
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
			m.Sha1Sum = append(m.Sha1Sum[:0], dAtA[iNdEx:postIndex]...)
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
				b := dAtA[iNdEx]
				iNdEx++
				m.Size_ |= (int64(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(dAtA[iNdEx:])
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
func (m *FileInfo) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
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
			b := dAtA[iNdEx]
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
				b := dAtA[iNdEx]
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
				b := dAtA[iNdEx]
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
			if err := m.Digests.Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
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
				b := dAtA[iNdEx]
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
			m.ModuleType = string(dAtA[iNdEx:postIndex])
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
				b := dAtA[iNdEx]
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
			if err := m.Chunks[len(m.Chunks)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(dAtA[iNdEx:])
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
func (m *FileInfo_Digests) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
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
			b := dAtA[iNdEx]
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
				b := dAtA[iNdEx]
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
			m.Sha1 = append(m.Sha1[:0], dAtA[iNdEx:postIndex]...)
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
				b := dAtA[iNdEx]
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
			m.Md5 = append(m.Md5[:0], dAtA[iNdEx:postIndex]...)
			if m.Md5 == nil {
				m.Md5 = []byte{}
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProtos(dAtA[iNdEx:])
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
func skipProtos(dAtA []byte) (n int, err error) {
	l := len(dAtA)
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
			b := dAtA[iNdEx]
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
				if dAtA[iNdEx-1] < 0x80 {
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
				b := dAtA[iNdEx]
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
					b := dAtA[iNdEx]
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
				next, err := skipProtos(dAtA[start:])
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

func init() { proto.RegisterFile("protos.proto", fileDescriptor_protos_5d00d5d0475a66b2) }

var fileDescriptor_protos_5d00d5d0475a66b2 = []byte{
	// 314 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x4c, 0x51, 0xb1, 0x4e, 0xf3, 0x30,
	0x18, 0xf4, 0xf7, 0xa7, 0x6a, 0xff, 0xba, 0x1d, 0xc0, 0x93, 0xd5, 0xe1, 0x23, 0xea, 0x14, 0x06,
	0x52, 0xb5, 0x08, 0x89, 0x19, 0x10, 0x12, 0x62, 0xb3, 0xd8, 0x51, 0xdb, 0xa4, 0x49, 0x44, 0x53,
	0x57, 0x38, 0x19, 0xca, 0xc4, 0x23, 0x30, 0xf2, 0x08, 0x3c, 0x0a, 0x1b, 0x1d, 0x3b, 0x12, 0x67,
	0x61, 0xec, 0x23, 0xa0, 0xd8, 0x09, 0x30, 0xf9, 0xee, 0xf3, 0x7d, 0xe7, 0x3b, 0x99, 0xf6, 0xd7,
	0x8f, 0x32, 0x93, 0xca, 0x37, 0x07, 0x6b, 0x5b, 0x36, 0x38, 0x89, 0x92, 0x2c, 0xce, 0x67, 0xfe,
	0x5c, 0xa6, 0xa3, 0x48, 0x46, 0x72, 0x64, 0xe6, 0xb3, 0x7c, 0x61, 0x98, 0x21, 0x06, 0xd9, 0xb5,
	0xe1, 0x2d, 0xed, 0x5e, 0x27, 0xcb, 0xf0, 0x32, 0xce, 0x57, 0x0f, 0xec, 0x80, 0x3a, 0x8b, 0x24,
	0xe0, 0xe0, 0x82, 0xd7, 0x15, 0x15, 0x64, 0x9c, 0x76, 0x54, 0x3c, 0x1d, 0xab, 0x3c, 0xe5, 0xff,
	0x5c, 0xf0, 0xfa, 0xa2, 0xa1, 0x8c, 0xd1, 0x96, 0x4a, 0x9e, 0x42, 0xee, 0xb8, 0xe0, 0x39, 0xc2,
	0xe0, 0xe1, 0x07, 0xd0, 0xff, 0x95, 0xdb, 0xcd, 0x6a, 0x21, 0x7f, 0x04, 0xf0, 0x2b, 0x60, 0x13,
	0xda, 0x09, 0x92, 0x28, 0x54, 0x99, 0x32, 0x76, 0xbd, 0x09, 0xf7, 0xeb, 0x12, 0xcd, 0x9a, 0x7f,
	0x65, 0xef, 0x45, 0x23, 0x64, 0x47, 0xb4, 0x97, 0xca, 0x20, 0x5f, 0x86, 0xf7, 0xd9, 0x66, 0x6d,
	0xdf, 0xeb, 0x0a, 0x6a, 0x47, 0x77, 0x9b, 0x75, 0xc8, 0x8e, 0x69, 0x7b, 0x5e, 0xc5, 0x57, 0xbc,
	0xe5, 0x3a, 0x5e, 0x6f, 0x72, 0xf8, 0xd7, 0xd3, 0x14, 0x13, 0xb5, 0x60, 0x30, 0xa2, 0x9d, 0xda,
	0xdf, 0xc4, 0x8b, 0xa7, 0x63, 0x13, 0xaf, 0x2f, 0x0c, 0xae, 0xfa, 0xa7, 0xc1, 0x59, 0xdd, 0xb4,
	0x82, 0x17, 0xe7, 0xdb, 0x02, 0xc9, 0xae, 0x40, 0xb2, 0x2f, 0x10, 0x9e, 0x35, 0xc2, 0x9b, 0x46,
	0x78, 0xd7, 0x08, 0x5b, 0x8d, 0xf0, 0xa9, 0x11, 0xbe, 0x34, 0x92, 0xbd, 0x46, 0x78, 0x29, 0x91,
	0xbc, 0x96, 0x48, 0xb6, 0x25, 0x92, 0x5d, 0x89, 0x64, 0x66, 0xff, 0xe3, 0xf4, 0x3b, 0x00, 0x00,
	0xff, 0xff, 0x3b, 0x15, 0x03, 0x0f, 0xa6, 0x01, 0x00, 0x00,
}
