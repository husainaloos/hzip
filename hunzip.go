package hzip

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
)

var (
	ErrBadHeader = errors.New("hunzip: bad header")
)

const (
	FTEXT    = 1 << 0
	FHCRC    = 1 << 1
	FEXTRA   = 1 << 2
	FNAME    = 1 << 3
	FCOMMENT = 1 << 4
)

var (
	le = binary.LittleEndian
)

type HuffmanTree struct {
	code int // -1 if nothing
	zero *HuffmanTree
	one  *HuffmanTree
}

func (ht *HuffmanTree) pp(p uint) {
	if ht.zero == nil && ht.one == nil {
		fmt.Printf("%d:%d\n", p, ht.code)
		return
	}
	pp := p << 1
	if ht.zero != nil {
		ht.zero.pp(pp)
	}
	if ht.one != nil {
		ht.one.pp(pp | 1)
	}
}

func (ht *HuffmanTree) Print() {
	ht.pp(0)
}

func buildHuffmanTree(m []uint) *HuffmanTree {
	maxBitLength := uint(0)
	maxCode := 0
	for k, v := range m {
		if maxBitLength < v {
			maxBitLength = v
		}
		if v > 0 && k > maxCode {
			maxCode = k
		}
	}

	blcount := make([]int, maxBitLength+1)
	for _, v := range m {
		blcount[v]++
	}

	code := 0
	nextCode := make([]int, maxBitLength+1)
	for b := uint(1); b <= maxBitLength; b++ {
		code = (code + blcount[b-1]) << 1
		if blcount[b] > 0 {
			nextCode[b] = code
		}
	}

	for k, v := range blcount {
		fmt.Printf("[%d:%d]", k, v)
	}

	fmt.Println("")

	// for k, v := range nextCode {
	// 	fmt.Printf("[%d:%d]", k, v)
	// }
	// fmt.Println("")
	codes := make([]int, maxCode+1)
	for n := 0; n <= maxCode; n++ {
		ln := m[n]
		if ln > 0 {
			codes[n] = nextCode[ln]
			nextCode[ln]++
		}
	}

	root := &HuffmanTree{
		code: -1,
	}
	for n := 0; n <= maxCode; n++ {
		node := root

		if m[n] > 0 {
			for b := uint(m[n]); b > 0; b-- {
				if codes[n]&(1<<(b-1)) > 0 {
					if node.one == nil {
						node.one = &HuffmanTree{
							code: -1,
						}
					}
					node = node.one
				} else {
					if node.zero == nil {
						node.zero = &HuffmanTree{
							code: -1,
						}
					}
					node = node.zero
				}
			}
			node.code = n
		}
	}

	return root
}

type bitReader struct {
	r    *bufio.Reader
	buf  uint8
	mask uint8
}

func newBitReader(r io.Reader) (*bitReader, error) {
	rr := bufio.NewReader(r)
	buf, err := rr.ReadByte()
	if err != nil {
		return nil, err
	}
	mask := 0x01
	return &bitReader{
		r:    rr,
		buf:  uint8(buf),
		mask: uint8(mask),
	}, nil
}

func (br *bitReader) readBit() (uint8, error) {
	var bit uint8 = br.buf & br.mask
	if bit > 0 {
		bit = 1
	}
	br.mask = br.mask << 1
	if br.mask == 0 {
		br.mask = 0x01
		b, err := br.r.ReadByte()
		if err != nil {
			return 0, err
		}
		br.buf = uint8(b)
	}
	return bit, nil
}

func (br *bitReader) readBits(c uint) (uint, error) {
	var bits uint = 0

	for i := uint(0); i < c; i++ {
		b, err := br.readBit()
		if err != nil {
			return 0, err
		}
		bits |= (uint(b) << i)
	}

	return bits, nil
}

type ReaderBuilder struct {
	r *bufio.Reader

	Time     time.Time
	FileName string
	Comment  string
	OS       int
	CRC16    int
}

func NewReaderBuilder(r io.Reader) (*ReaderBuilder, error) {
	ret := &ReaderBuilder{
		r: bufio.NewReader(r),
	}
	if err := ret.readHeaders(); err != nil {
		return nil, err
	}
	return ret, nil
}

func (hunzip *ReaderBuilder) readHeaders() error {
	header := make([]byte, 10)
	n, err := hunzip.r.Read(header)
	if err != nil || n != 10 {
		return ErrBadHeader
	}

	if header[0] != 0x1f || header[1] != 0x8b || header[2] != 8 {
		return ErrBadHeader
	}

	flg := header[3]

	if t := le.Uint32(header[4:8]); t > 0 {
		hunzip.Time = time.Unix(int64(t), 0)
	}
	hunzip.OS = int(header[9])

	if flg&FEXTRA > 0 {
		// read and ignore the content
		b := make([]byte, 2)
		n, err := hunzip.r.Read(b)
		if err != nil || n != 2 {
			return ErrBadHeader
		}
		xlen := le.Uint16(b)
		b = make([]byte, xlen)
		n, err = hunzip.r.Read(b)
		if err != nil || n != 2 {
			return ErrBadHeader
		}
	}
	if flg&FNAME > 0 {
		hunzip.FileName, err = hunzip.r.ReadString(0x00)
		if err != nil {
			return ErrBadHeader
		}
	}
	if flg&FCOMMENT > 0 {
		hunzip.Comment, err = hunzip.r.ReadString(0x00)
		if err != nil {
			return ErrBadHeader
		}
	}
	if flg&FHCRC > 0 {
		b := make([]byte, 2)
		n, err := hunzip.r.Read(b)
		if err != nil || n != 2 {
			return ErrBadHeader
		}
		hunzip.CRC16 = int(le.Uint16(b))
	}

	// log.Printf("time: %s, name: %s, comment: %s, OS: %d, CRC16: %d",
	// 	hunzip.Time,
	// 	hunzip.FileName,
	// 	hunzip.Comment,
	// 	hunzip.OS,
	// 	hunzip.CRC16)

	return nil
}

func (rb *ReaderBuilder) Reader() (io.Reader, error) {
	b, err := rb.unzip()
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func (br *ReaderBuilder) unzip() ([]byte, error) {
	var bFinal uint8 = 1
	ret := make([]byte, 0)
	r, err := newBitReader(br.r)
	if err != nil {
		return nil, err
	}
	for bFinal > 0 {
		bFinal, err = r.readBit()
		if err != nil {
			return nil, err
		}
		bType, err := r.readBits(2)
		if err != nil {
			return nil, err
		}
		// log.Printf("bType: %d", bType)
		switch bType {
		case 0:
			return nil, errors.New("unsupported uncompressed")
		case 1:
			return nil, errors.New("unspported compression with fixed huffman")
		case 2:
			b, err := br.unzipDynamicHuffman(r)
			if err != nil {
				return nil, err
			}
			ret = append(ret, b...)
		case 3:
			return nil, errors.New("bad bType")
		}

	}
	return ret, nil
}

func (br *ReaderBuilder) unzipDynamicHuffman(r *bitReader) ([]byte, error) {
	hlit, err := r.readBits(5)
	if err != nil {
		return nil, err
	}
	hdist, err := r.readBits(5)
	if err != nil {
		return nil, err
	}
	hclen, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	// log.Printf("hlit=%d, hdist=%d, hclen=%d", hlit, hdist, hclen)

	offset := []int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}
	var clength [19]uint
	for i := uint(0); i < hclen+4; i++ {
		clength[offset[i]], err = r.readBits(3)
		if err != nil {
			return nil, err
		}
	}

	tree := buildHuffmanTree(clength[:])
	// tree.Print()
	// log.Println("----")

	state := tree
	alphabet := make([]uint, hlit+hdist+258)
	var i uint = 0
	for i < hlit+hdist+258 {
		b, err := r.readBit()
		if err != nil {
			return nil, err
		}
		if b > 0 {
			state = state.one
		} else {
			state = state.zero
		}
		if state.zero == nil && state.one == nil {
			if state.code > 15 {
				var repeat uint
				var err error
				switch state.code {
				case 16:
					repeat, err = r.readBits(2)
					if err != nil {
						return nil, err
					}
					repeat += 3
				case 17:
					repeat, err = r.readBits(3)
					if err != nil {
						return nil, err
					}
					repeat += 3
				case 18:
					repeat, err = r.readBits(7)
					if err != nil {
						return nil, err
					}
					repeat += 11
				default:
					return nil, errors.New("can't be more than 18")
				}
				for repeat > 0 {
					repeat--
					if state.code == 16 {
						alphabet[i] = alphabet[i-1]
					} else {
						alphabet[i] = 0
					}
					i++
				}
			} else {
				alphabet[i] = uint(state.code)
				i++
			}
			state = tree
		}
	}

	for k, v := range alphabet {
		fmt.Printf("alphabet[%d]=%d\n", k, v)
	}

	literalRoot := buildHuffmanTree(alphabet[:hlit+257])
	distanceRoot := buildHuffmanTree(alphabet[hlit+257:])

	// literalRoot.Print()
	// log.Println("----")
	// distanceRoot.Print()
	// log.Println("----")

	ela := []int{11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227}
	eda := []int{4, 6, 8, 12, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576}
	node := literalRoot
	buf := make([]uint8, 1000000)
	bufi := 0
	stopCode := 0
	for stopCode == 0 {
		b, err := r.readBit()
		if err != nil {
			return nil, err
		}
		if b > 0 {
			if node.one == nil {
				return nil, errors.New("one can't be nil")
			}
			node = node.one
		} else {
			if node.zero == nil {
				return nil, errors.New("zero can't be nil")
			}
			node = node.zero
		}

		if node.zero == nil && node.one == nil {
			if node.code >= 286 {
				return nil, errors.New("code is too big")
			} else if node.code < 256 {
				buf[bufi] = uint8(node.code)
				bufi++
			} else if node.code == 256 {
				stopCode = -1
				break
			} else if node.code > 256 {
				var length int
				if node.code < 265 {
					length = node.code - 254
				} else if node.code < 285 {
					eb, err := r.readBits(uint((node.code - 261) / 4))
					if err != nil {
						return nil, err
					}
					length = int(eb) + ela[node.code-265]
				} else {
					length = 258
				}

				if distanceRoot == nil {
					panic("no distance Root")
				}

				node = distanceRoot
				for node.zero != nil || node.one != nil {
					b, err := r.readBit()
					if err != nil {
						return nil, err
					}
					if b > 0 {
						node = node.one
					} else {
						node = node.zero
					}
				}

				dist := node.code

				if dist > 3 {
					eb, err := r.readBits(uint((dist - 2) / 2))
					if err != nil {
						return nil, err
					}
					dist = int(eb) + eda[dist-4]
				}
				fmt.Printf("dist=%d\n", dist)
				bp := bufi - dist - 1
				for length > 0 {
					length--
					buf[bufi] = uint8(buf[bp])
					bufi++
					bp++
				}
			}
			//printBuffer(buf)
			node = literalRoot
		}
	}

	printBuffer(buf)
	return []byte{}, errors.New("done")
}

func printBuffer(b []uint8) {
	s := ""
	for _, v := range b {
		s += string(byte(uint8(v)))
	}
	log.Printf("buf: %s", s)
}
