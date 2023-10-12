/* crypto/rand using math/rand as interface (C) Stefan Nilsson
 * https://yourbasic.org/golang/crypto-rand-int/
 * Modified by SA6MWA with a mutex lock to be goroutine-safe and packaged as
 * github.com/sa6mwa/gotostash/pkg/crand
 */

package crand

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"sync"
)

// There is no seeding required in this implementation so no need to export a
// new source like with math/rand, but this will have to change if we add
// another PRNG. The API should then be backward compatible with the
// crypto/rand implementation as default.
var gsrc = cryptoRandSource{&sync.Mutex{}}
var gr = rand.New(gsrc)

// Not sure if we want to export some of these structs in the future, but
// currently the package only exports the primary functionality.
type cryptoRandSource struct {
	*sync.Mutex
}

func (s cryptoRandSource) Seed(seed int64) {
	// no seeding, already handled by the OS
}
func (s cryptoRandSource) Int63() int64 {
	return int64(s.Uint64() & ^uint64(1<<63))
}
func (s cryptoRandSource) Uint64() (v uint64) {
	s.Lock()
	err := binary.Read(cryptoRand.Reader, binary.BigEndian, &v)
	s.Unlock()
	if err != nil {
		panic(err)
	}
	return // automatically implies that v is returned
}

func (s cryptoRandSource) Read(p []byte) (n int, err error) {
	s.Lock()
	err = binary.Read(cryptoRand.Reader, binary.BigEndian, &p)
	s.Unlock()
	return len(p), err
}

func (s cryptoRandSource) ReadRunes(p []rune) (n int, err error) {
	s.Lock()
	err = binary.Read(cryptoRand.Reader, binary.BigEndian, &p)
	s.Unlock()
	return len(p), err
}

// These functions are frontends to math/rand...
func Seed(seed int64)                       { gsrc.Seed(seed) }
func Int63() int64                          { return gsrc.Int63() }
func Uint32() uint32                        { return gr.Uint32() }
func Uint64() uint64                        { return gsrc.Uint64() }
func Int31() int32                          { return gr.Int31() }
func Int() int                              { return gr.Int() }
func Int63n(n int64) int64                  { return gr.Int63n(n) }
func Int31n(n int32) int32                  { return gr.Int31n(n) }
func Intn(n int) int                        { return gr.Intn(n) }
func Float64() float64                      { return gr.Float64() }
func Float32() float32                      { return gr.Float32() }
func Perm(n int) []int                      { return gr.Perm(n) }
func Shuffle(n int, swap func(i, j int))    { gr.Shuffle(n, swap) }
func Read(p []byte) (n int, err error)      { return gsrc.Read(p) }
func ReadRunes(p []rune) (n int, err error) { return gsrc.ReadRunes(p) }
func NormFloat64() float64                  { return gr.NormFloat64() }
func ExpFloat64() float64                   { return gr.ExpFloat64() }
