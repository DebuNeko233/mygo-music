package player

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
)

// A decoder never sees zeros from an unreceived range; it waits until the bytes actually arrive.
func TestDownloadReaderAtWaitsForSparseRange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := bytes.Repeat([]byte{1, 3, 5, 7}, 32768)
		d := downloaded(data[:64])
		d.done = false
		d.length = int64(len(data))
		defer d.Close()
		result := make(chan error, 1)
		go func() {
			b := make([]byte, 16)
			n, err := d.ReadAt(b, 90000)
			if err == nil && (n != 16 || !bytes.Equal(b, data[90000:90016])) {
				err = errors.New("sparse bytes decoded as data")
			}
			result <- err
		}()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("read did not wait: %v", err)
		default:
		}
		if err := d.writeAt(data[90000:90016], 90000); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	})
}

// Header read-ahead uses already available bytes without waiting to fill a buffer, and cancellation wakes blocked reads.
func TestDownloadReaderAtPrefixAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := downloaded([]byte("abcdefghijklmnop"))
		d.done = false
		d.length = 1 << 20
		r := &bufferedReaderAt{source: d}
		result := make(chan error, 1)
		go func() {
			b := make([]byte, 4)
			_, err := r.ReadAt(b, 4)
			if err == nil && string(b) != "efgh" {
				err = errors.New("wrong header")
			}
			result <- err
		}()
		synctest.Wait()
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("small read waited for the complete read-ahead buffer")
		}
		go func() { _, err := d.ReadAt(make([]byte, 4), 80000); result <- err }()
		synctest.Wait()
		d.Close()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled read: %v", err)
		}
	})
}

// Indexed decoders waiting for an unknown length also exit when their download is cancelled.
func TestDownloadDecoderReaderCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := downloaded(nil)
		d.done = false
		d.length = -1
		result := make(chan error, 1)
		go func() { _, err := d.decoderReader(); result <- err }()
		synctest.Wait()
		d.Close()
		synctest.Wait()
		if err := <-result; !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("cancelled metadata read: %v", err)
		}
	})
}
