package player

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Keep this out of the default suite: it deliberately creates thousands of
// decoder instances to make leaked WASM allocations visible at process scale.
func TestLocalM4AMemoryProbe(t *testing.T) {
	if os.Getenv("M4A_MEMORY_PROBE") == "" {
		t.Skip("memory probe disabled")
	}
	path := filepath.Join(t.TempDir(), "local.m4a")
	if err := os.WriteFile(path, testAudio(t, "seek-he.m4a"), 0600); err != nil {
		t.Fatal(err)
	}
	measure := func(label string) {
		for range 5 {
			runtime.GC()
		}
		var heap runtime.MemStats
		runtime.ReadMemStats(&heap)
		var counters processMemoryCounters
		counters.cb = uint32(unsafe.Sizeof(counters))
		r, _, err := getProcessMemoryInfo.Call(uintptr(mustCurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
		if r == 0 {
			t.Fatal(err)
		}
		t.Logf("%s: GoHeap=%.2fMiB WorkingSet=%.2fMiB PrivateBytes=%.2fMiB", label, float64(heap.HeapAlloc)/(1<<20), float64(counters.workingSetSize)/(1<<20), float64(counters.pagefileUsage)/(1<<20))
	}
	measure("warmup")
	for i := 1; i <= 5000; i++ {
		prepared, err := PrepareLocalAudio(path)
		if err != nil {
			t.Fatal(err)
		}
		prepared.Close()
		if i%1000 == 0 {
			measure(fmt.Sprint("closed streams ", i))
		}
	}
}

type processMemoryCounters struct {
	cb, pageFaultCount                                          uint32
	peakWorkingSetSize, workingSetSize, quotaPeakPagedPoolUsage uint64
	quotaPagedPoolUsage, quotaPeakNonPagedPoolUsage             uint64
	quotaNonPagedPoolUsage, pagefileUsage, peakPagefileUsage    uint64
}

var getProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func mustCurrentProcess() windows.Handle {
	h, err := windows.GetCurrentProcess()
	if err != nil {
		panic(err)
	}
	return h
}
