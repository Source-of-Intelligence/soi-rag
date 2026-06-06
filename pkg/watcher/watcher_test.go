package watcher

import (
	"testing"
)

// TestNanoTime 验证 nanoTime() 返回非零值
func TestNanoTime(t *testing.T) {
	n1 := nanoTime()
	if n1 == 0 {
		t.Error("nanoTime() 返回值不应为 0")
	}

	// 连续调用两次，第二次应大于等于第一次（时间单调递增）
	n2 := nanoTime()
	if n2 < n1 {
		t.Errorf("nanoTime() 应单调递增: n1=%d, n2=%d", n1, n2)
	}

	// 多次调用验证稳定性
	for i := 0; i < 10; i++ {
		n := nanoTime()
		if n == 0 {
			t.Errorf("第 %d 次调用 nanoTime() 返回 0", i+1)
		}
	}
}

// TestNanoTimePositive 验证 nanoTime() 返回正值
func TestNanoTimePositive(t *testing.T) {
	n := nanoTime()
	if n <= 0 {
		t.Errorf("nanoTime() 应返回正值, 实际=%d", n)
	}
}
