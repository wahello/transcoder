package mfr

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type queueSuite struct {
	suite.Suite
	popClaim1,
	popClaim2,
	popClaim3 *claim
	q *Queue
}

type claim struct {
	sdHash, url string
}

func TestQueueSuite(t *testing.T) {
	suite.Run(t, new(queueSuite))
}

func (s *queueSuite) SetupTest() {
	rand.Seed(time.Now().UnixNano())

	q := NewQueue()

	popClaim1 := &claim{randomString(25), randomString(96)}
	popClaim2 := &claim{randomString(25), randomString(96)}
	popClaim3 := &claim{randomString(25), randomString(96)}

	wg := &sync.WaitGroup{}
	wg.Add(4)
	go func() {
		defer wg.Done()
		for range [10000]byte{} {
			q.Hit(popClaim1.url, popClaim1)
			q.Peek()
		}
	}()
	go func() {
		defer wg.Done()
		for range [10000]byte{} {
			q.Hit(popClaim2.url, popClaim2)
			q.Peek()
		}
	}()
	go func() {
		defer wg.Done()
		for range [9000]byte{} {
			q.Hit(popClaim3.url, popClaim3)
			q.Peek()
		}
	}()
	go func() {
		defer wg.Done()
		for range [100000]byte{} {
			c := &claim{randomString(25), randomString(96)}
			q.Peek()
			q.Hit(c.url, c)
		}
	}()
	wg.Wait()
	s.popClaim1 = popClaim1
	s.popClaim2 = popClaim2
	s.popClaim3 = popClaim3
	s.q = q
}

func (s *queueSuite) TestPeek() {
	item1 := s.q.Pop()
	s.Require().NotNil(item1)
	s.Equal(s.popClaim1.url, item1.key)
	s.Equal(s.popClaim1, item1.Value.(*claim))
	s.EqualValues(10000, item1.Hits())

	item2 := s.q.Pop()
	s.Require().NotNil(item2)
	s.Equal(s.popClaim2.url, item2.key)
	s.Equal(s.popClaim2, item2.Value.(*claim))
	s.EqualValues(10000, item2.Hits())

	item3 := s.q.Pop()
	s.Require().NotNil(item3)
	s.Equal(s.popClaim3.url, item3.key)
	s.Equal(s.popClaim3, item3.Value.(*claim))
	s.EqualValues(9000, item3.Hits())

	s.EqualValues(129000, s.q.hits)
}

func (s *queueSuite) TestRelease() {
	item := s.q.Pop()
	s.Require().NotNil(item)
	s.q.Release(item.key)

	item2 := s.q.Pop()
	s.Equal(item, item2)
}

func (s *queueSuite) TestFold() {
	item := s.q.Pop()
	s.Require().NotNil(item)

	s.q.Fold(item.key)
	item2 := s.q.Pop()
	s.NotEqual(item, item2)
}

func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}