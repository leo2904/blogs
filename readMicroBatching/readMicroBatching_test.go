package readMicroBatching

import (
	"context"
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

const (
	NumberOfReadInitiators = 300
	ChannelBufferSize      = 10
	MaxPkRange             = 350_000_000
	NumberOfReads          = 3_000_000
	batchedRead            = true
	SequenceLength         = 10
	statisticsFileName     = "statistics.csv"
)

type averageDurationType struct {
	cumulativeDuration int64
	counter            int64
	lock               sync.Mutex
}

var averageDurationCalculator *averageDurationType

func TestRead(t *testing.T) {
	averageDurationCalculator = &averageDurationType{}
	InitReading()
	readExitWG := sync.WaitGroup{}
	pkChan := make(chan string, ChannelBufferSize)
	readExitWG.Add(NumberOfReadInitiators)
	for i := 0; i < NumberOfReadInitiators; i++ {
		if batchedRead {
			go batchReader(pkChan, &readExitWG)
		} else {
			go discreteReader(pkChan, &readExitWG)
		}
	}
	start := time.Now()
	for i := 0; i < NumberOfReads; i += SequenceLength {
		r := rand.Int31n(MaxPkRange)
		for j := 0; j < SequenceLength; j++ {
			pk := fmt.Sprintf("%020d", r)
			pkChan <- pk
		}
	}
	close(pkChan)
	readExitWG.Wait()
	collectStats(time.Now().Sub(start), NumberOfReads, NumberOfReadsPerBatch, batchedRead)
}

func batchReader(pkChan chan string, exitWG *sync.WaitGroup) {
	defer exitWG.Done()
	for {
		pk, moreEntries := <-pkChan
		if !moreEntries {
			break
		}
		startRead := time.Now()
		entry, err := ReadEntry(pk)
		panicIfError(err)
		averageDurationCalculator.addDuration(startRead)
		if entry.Pk != pk {
			panic("entry do not match request")
		}
	}
}

func discreteReader(pkChan chan string, exitWG *sync.WaitGroup) {
	var readPk, readPayload string
	readEntrySQL := "select pk,payload from random_read_test where pk = $1"
	defer exitWG.Done()
	for {
		pk, moreEntries := <-pkChan
		if !moreEntries {
			break
		}
		startRead := time.Now()
		err := db.QueryRow(context.Background(), readEntrySQL, pk).Scan(&readPk, &readPayload)
		panicIfError(err)
		averageDurationCalculator.addDuration(startRead)
		if readPk != pk {
			panic("entry does not match request")
		}
	}
}

func collectStats(duration time.Duration, readNum, batchSize int, batched bool) {
	f, err := os.OpenFile(statisticsFileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0777)
	panicIfError(err)
	w := csv.NewWriter(f)
	batchedStr := "  discrete  "
	if batched {
		batchedStr = " batched   "
	}
	durationStr := fmt.Sprintf("%v", duration)
	batchSizeStr := fmt.Sprintf("% 10d", batchSize)
	readNumStr := fmt.Sprintf("% 10d", readNum)
	averageReadDurationStr := fmt.Sprintf("  %v  ", averageDurationCalculator.getAverage())
	line := []string{durationStr, averageReadDurationStr, batchSizeStr, readNumStr, batchedStr}
	err = w.Write(line)
	panicIfError(err)
	w.Flush()
	f.Close()
}

func (c *averageDurationType) addDuration(start time.Time) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.counter++
	c.cumulativeDuration += int64(time.Now().Sub(start))
}

func (c *averageDurationType) getAverage() time.Duration {
	c.lock.Lock()
	defer c.lock.Unlock()
	average := time.Duration(c.cumulativeDuration / c.counter)
	return average
}
