package client

import "time"

// Backoff 指数退避：Min, 2*Min, ... 封顶 Max；Reset 后重新开始。
type Backoff struct {
	Min, Max time.Duration
	cur      time.Duration
}

func (b *Backoff) Next() time.Duration {
	if b.cur == 0 {
		b.cur = b.Min
	} else {
		b.cur *= 2
		if b.cur > b.Max {
			b.cur = b.Max
		}
	}
	return b.cur
}

func (b *Backoff) Reset() { b.cur = 0 }
