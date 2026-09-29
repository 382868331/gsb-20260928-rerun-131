package ir

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type Op byte

const (
	OpConst Op = iota
	OpLoad
	OpStore
	OpPop
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpNeg
	OpNot
	OpEq
	OpNe
	OpLt
	OpGt
	OpLe
	OpGe
	OpJump
	OpJumpIfFalse
	OpJumpIfTrue
	OpPopJumpIfFalse
	OpCall
	OpCallBuiltin
	OpReturn
	OpHalt
)

type Instr struct {
	Op   Op
	Arg  uint32
	Line uint32
}

type Function struct {
	Name      string
	Params    uint32
	NumLocals uint32
	Consts    []int64
	Code      []Instr
}

type Program struct {
	Main  uint32
	Funcs []*Function
}

func (f *Function) AddConst(v int64) uint32 {
	for i, c := range f.Consts {
		if c == v {
			return uint32(i)
		}
	}
	f.Consts = append(f.Consts, v)
	return uint32(len(f.Consts) - 1)
}

var magic = [4]byte{'T', 'L', 'I', 'R'}

const version uint32 = 1

// Encode writes a compact, versioned binary representation of the IR.
func (p *Program) Encode(w io.Writer) error {
	bw := &writer{w: w}
	bw.bytes(magic[:])
	bw.u32(version)
	bw.u32(p.Main)
	bw.u32(uint32(len(p.Funcs)))
	for _, f := range p.Funcs {
		bw.str(f.Name)
		bw.u32(f.Params)
		bw.u32(f.NumLocals)
		bw.u32(uint32(len(f.Consts)))
		for _, c := range f.Consts {
			bw.i64(c)
		}
		bw.u32(uint32(len(f.Code)))
		for _, ins := range f.Code {
			bw.byte(byte(ins.Op))
			bw.u32(ins.Arg)
			bw.u32(ins.Line)
		}
	}
	return bw.err
}

func Decode(r io.Reader) (*Program, error) {
	br := &reader{r: r}
	m := make([]byte, 4)
	br.bytes(m)
	if br.err == nil && [4]byte{m[0], m[1], m[2], m[3]} != magic {
		return nil, errors.New("not a TL IR file: bad magic number")
	}
	v := br.u32()
	if br.err == nil && v != version {
		return nil, fmt.Errorf("unsupported TL IR version %d, expected %d", v, version)
	}
	p := &Program{Main: br.u32()}
	n := br.u32()
	if br.err != nil {
		return nil, br.err
	}
	for i := uint32(0); i < n; i++ {
		f := &Function{}
		f.Name = br.str()
		f.Params = br.u32()
		f.NumLocals = br.u32()
		nc := br.u32()
		for j := uint32(0); j < nc; j++ {
			f.Consts = append(f.Consts, br.i64())
		}
		ni := br.u32()
		for j := uint32(0); j < ni; j++ {
			f.Code = append(f.Code, Instr{Op: Op(br.byte()), Arg: br.u32(), Line: br.u32()})
		}
		p.Funcs = append(p.Funcs, f)
	}
	if br.err != nil {
		return nil, br.err
	}
	if int(p.Main) >= len(p.Funcs) {
		return nil, errors.New("invalid IR: main function index out of range")
	}
	return p, nil
}

type writer struct {
	w   io.Writer
	err error
	buf [8]byte
}

func (w *writer) byte(b byte) { w.bytes([]byte{b}) }
func (w *writer) bytes(b []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(b)
}
func (w *writer) u32(v uint32) {
	binary.LittleEndian.PutUint32(w.buf[:4], v)
	w.bytes(w.buf[:4])
}
func (w *writer) i64(v int64) {
	binary.LittleEndian.PutUint64(w.buf[:8], uint64(v))
	w.bytes(w.buf[:8])
}
func (w *writer) str(s string) {
	w.u32(uint32(len(s)))
	w.bytes([]byte(s))
}

type reader struct {
	r   io.Reader
	err error
	buf [8]byte
}

func (r *reader) byte() byte { r.bytes(r.buf[:1]); return r.buf[0] }
func (r *reader) bytes(b []byte) {
	if r.err != nil {
		return
	}
	_, r.err = io.ReadFull(r.r, b)
}
func (r *reader) u32() uint32 {
	r.bytes(r.buf[:4])
	if r.err != nil {
		return 0
	}
	return binary.LittleEndian.Uint32(r.buf[:4])
}
func (r *reader) i64() int64 {
	r.bytes(r.buf[:8])
	if r.err != nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(r.buf[:8]))
}
func (r *reader) str() string {
	n := r.u32()
	if n > 1<<20 {
		r.err = errors.New("invalid IR: string too large")
		return ""
	}
	b := make([]byte, n)
	r.bytes(b)
	return string(b)
}
