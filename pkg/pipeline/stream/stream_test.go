// Copyright © 2022 Meroxa, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stream_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/conduitio/conduit/pkg/connector"
	connmock "github.com/conduitio/conduit/pkg/connector/mock"
	"github.com/conduitio/conduit/pkg/foundation/csync"
	"github.com/conduitio/conduit/pkg/foundation/ctxutil"
	"github.com/conduitio/conduit/pkg/foundation/log"
	"github.com/conduitio/conduit/pkg/foundation/metrics/noop"
	"github.com/conduitio/conduit/pkg/pipeline/stream"
	"github.com/conduitio/conduit/pkg/plugin"
	"github.com/conduitio/conduit/pkg/processor"
	procmock "github.com/conduitio/conduit/pkg/processor/mock"
	"github.com/conduitio/conduit/pkg/record"
	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
)

func Example_simpleStream() {
	ctx, killAll := context.WithCancel(context.Background())
	defer killAll()

	logger := newLogger()
	ctrl := gomockCtrl(logger)

	node1 := &stream.SourceNode{
		Name:          "generator",
		Source:        generatorSource(ctrl, logger, "generator", 10, time.Millisecond*10),
		PipelineTimer: noop.Timer{},
	}
	node2 := &stream.SourceAckerNode{
		Name:   "generator-acker",
		Source: node1.Source,
	}
	node3 := &stream.DestinationNode{
		Name:           "printer",
		Destination:    printerDestination(ctrl, logger, "printer"),
		ConnectorTimer: noop.Timer{},
	}
	node4 := &stream.DestinationAckerNode{
		Name:        "printer-acker",
		Destination: node3.Destination,
	}

	stream.SetLogger(node1, logger)
	stream.SetLogger(node2, logger)
	stream.SetLogger(node3, logger)
	stream.SetLogger(node4, logger)

	// put everything together
	node2.Sub(node1.Pub())
	node3.Sub(node2.Pub())
	node4.Sub(node3.Pub())

	var wg sync.WaitGroup
	wg.Add(4)
	go runNode(ctx, &wg, node4)
	go runNode(ctx, &wg, node3)
	go runNode(ctx, &wg, node2)
	go runNode(ctx, &wg, node1)

	// stop node after 150ms, which should be enough to process the 10 messages
	time.AfterFunc(150*time.Millisecond, func() { _ = node1.Stop(ctx, nil) })
	// give the node some time to process the records, plus a bit of time to stop
	if (*csync.WaitGroup)(&wg).WaitTimeout(ctx, time.Second) != nil {
		killAll()
	} else {
		logger.Info(ctx).Msg("finished successfully")
	}

	// Output:
	// DBG got record message_id=generator/1 node_id=printer
	// DBG received ack message_id=generator/1 node_id=generator
	// DBG got record message_id=generator/2 node_id=printer
	// DBG received ack message_id=generator/2 node_id=generator
	// DBG got record message_id=generator/3 node_id=printer
	// DBG received ack message_id=generator/3 node_id=generator
	// DBG got record message_id=generator/4 node_id=printer
	// DBG received ack message_id=generator/4 node_id=generator
	// DBG got record message_id=generator/5 node_id=printer
	// DBG received ack message_id=generator/5 node_id=generator
	// DBG got record message_id=generator/6 node_id=printer
	// DBG received ack message_id=generator/6 node_id=generator
	// DBG got record message_id=generator/7 node_id=printer
	// DBG received ack message_id=generator/7 node_id=generator
	// DBG got record message_id=generator/8 node_id=printer
	// DBG received ack message_id=generator/8 node_id=generator
	// DBG got record message_id=generator/9 node_id=printer
	// DBG received ack message_id=generator/9 node_id=generator
	// DBG got record message_id=generator/10 node_id=printer
	// DBG received ack message_id=generator/10 node_id=generator
	// INF stopping source connector component=SourceNode node_id=generator
	// INF stopping source node component=SourceNode node_id=generator record_position=10
	// DBG incoming messages channel closed component=SourceAckerNode node_id=generator-acker
	// DBG incoming messages channel closed component=DestinationNode node_id=printer
	// DBG incoming messages channel closed component=DestinationAckerNode node_id=printer-acker
	// INF finished successfully
}

func Example_complexStream() {
	ctx, killAll := context.WithCancel(context.Background())
	defer killAll()

	logger := newLogger()
	ctrl := gomockCtrl(logger)

	var count int

	node1 := &stream.SourceNode{
		Name:          "generator1",
		Source:        generatorSource(ctrl, logger, "generator1", 10, time.Millisecond*10),
		PipelineTimer: noop.Timer{},
	}
	node2 := &stream.SourceAckerNode{
		Name:   "generator1-acker",
		Source: node1.Source,
	}
	node3 := &stream.SourceNode{
		Name:          "generator2",
		Source:        generatorSource(ctrl, logger, "generator2", 10, time.Millisecond*10),
		PipelineTimer: noop.Timer{},
	}
	node4 := &stream.SourceAckerNode{
		Name:   "generator2-acker",
		Source: node3.Source,
	}
	node5 := &stream.FaninNode{Name: "fanin"}
	node6 := &stream.ProcessorNode{
		Name:           "counter",
		Processor:      counterProcessor(ctrl, &count),
		ProcessorTimer: noop.Timer{},
	}
	node7 := &stream.FanoutNode{Name: "fanout"}
	node8 := &stream.DestinationNode{
		Name:           "printer1",
		Destination:    printerDestination(ctrl, logger, "printer1"),
		ConnectorTimer: noop.Timer{},
	}
	node9 := &stream.DestinationAckerNode{
		Name:        "printer1-acker",
		Destination: node8.Destination,
	}
	node10 := &stream.DestinationNode{
		Name:           "printer2",
		Destination:    printerDestination(ctrl, logger, "printer2"),
		ConnectorTimer: noop.Timer{},
	}
	node11 := &stream.DestinationAckerNode{
		Name:        "printer2-acker",
		Destination: node10.Destination,
	}

	// put everything together
	// this is the pipeline we are building
	// [1] -> [2] -\                       /-> [8] -> [9]
	//              |- [5] -> [6] -> [7] -|
	// [3] -> [4] -/                       \-> [10] -> [11]
	node2.Sub(node1.Pub())
	node4.Sub(node3.Pub())

	node5.Sub(node2.Pub())
	node5.Sub(node4.Pub())

	node6.Sub(node5.Pub())
	node7.Sub(node6.Pub())

	node8.Sub(node7.Pub())
	node10.Sub(node7.Pub())

	node9.Sub(node8.Pub())
	node11.Sub(node10.Pub())

	// run nodes
	nodes := []stream.Node{node1, node2, node3, node4, node5, node6, node7, node8, node9, node10, node11}

	var wg sync.WaitGroup
	wg.Add(len(nodes))
	for _, n := range nodes {
		stream.SetLogger(n, logger)
		go runNode(ctx, &wg, n)
	}

	// stop nodes after 250ms, which should be enough to process the 20 messages
	time.AfterFunc(
		250*time.Millisecond,
		func() {
			_ = node1.Stop(ctx, nil)
			_ = node3.Stop(ctx, nil)
		},
	)
	// give the nodes some time to process the records, plus a bit of time to stop
	if (*csync.WaitGroup)(&wg).WaitTimeout(ctx, time.Second) != nil {
		killAll()
	} else {
		logger.Info(ctx).Msgf("counter node counted %d messages", count)
		logger.Info(ctx).Msg("finished successfully")
	}

	// Unordered output:
	// DBG got record message_id=generator2/1 node_id=printer2
	// DBG got record message_id=generator2/1 node_id=printer1
	// DBG received ack message_id=generator2/1 node_id=generator2
	// DBG got record message_id=generator1/1 node_id=printer1
	// DBG got record message_id=generator1/1 node_id=printer2
	// DBG received ack message_id=generator1/1 node_id=generator1
	// DBG got record message_id=generator2/2 node_id=printer2
	// DBG got record message_id=generator2/2 node_id=printer1
	// DBG received ack message_id=generator2/2 node_id=generator2
	// DBG got record message_id=generator1/2 node_id=printer1
	// DBG got record message_id=generator1/2 node_id=printer2
	// DBG received ack message_id=generator1/2 node_id=generator1
	// DBG got record message_id=generator2/3 node_id=printer2
	// DBG got record message_id=generator2/3 node_id=printer1
	// DBG received ack message_id=generator2/3 node_id=generator2
	// DBG got record message_id=generator1/3 node_id=printer1
	// DBG got record message_id=generator1/3 node_id=printer2
	// DBG received ack message_id=generator1/3 node_id=generator1
	// DBG got record message_id=generator2/4 node_id=printer2
	// DBG got record message_id=generator2/4 node_id=printer1
	// DBG received ack message_id=generator2/4 node_id=generator2
	// DBG got record message_id=generator1/4 node_id=printer2
	// DBG got record message_id=generator1/4 node_id=printer1
	// DBG received ack message_id=generator1/4 node_id=generator1
	// DBG got record message_id=generator2/5 node_id=printer2
	// DBG got record message_id=generator2/5 node_id=printer1
	// DBG received ack message_id=generator2/5 node_id=generator2
	// DBG got record message_id=generator1/5 node_id=printer1
	// DBG got record message_id=generator1/5 node_id=printer2
	// DBG received ack message_id=generator1/5 node_id=generator1
	// DBG got record message_id=generator2/6 node_id=printer2
	// DBG got record message_id=generator2/6 node_id=printer1
	// DBG received ack message_id=generator2/6 node_id=generator2
	// DBG got record message_id=generator1/6 node_id=printer1
	// DBG got record message_id=generator1/6 node_id=printer2
	// DBG received ack message_id=generator1/6 node_id=generator1
	// DBG got record message_id=generator2/7 node_id=printer2
	// DBG got record message_id=generator2/7 node_id=printer1
	// DBG received ack message_id=generator2/7 node_id=generator2
	// DBG got record message_id=generator1/7 node_id=printer1
	// DBG got record message_id=generator1/7 node_id=printer2
	// DBG received ack message_id=generator1/7 node_id=generator1
	// DBG got record message_id=generator2/8 node_id=printer2
	// DBG got record message_id=generator2/8 node_id=printer1
	// DBG received ack message_id=generator2/8 node_id=generator2
	// DBG got record message_id=generator1/8 node_id=printer1
	// DBG got record message_id=generator1/8 node_id=printer2
	// DBG received ack message_id=generator1/8 node_id=generator1
	// DBG got record message_id=generator2/9 node_id=printer1
	// DBG got record message_id=generator2/9 node_id=printer2
	// DBG received ack message_id=generator2/9 node_id=generator2
	// DBG got record message_id=generator1/9 node_id=printer2
	// DBG got record message_id=generator1/9 node_id=printer1
	// DBG received ack message_id=generator1/9 node_id=generator1
	// DBG got record message_id=generator2/10 node_id=printer1
	// DBG got record message_id=generator2/10 node_id=printer2
	// DBG received ack message_id=generator2/10 node_id=generator2
	// DBG got record message_id=generator1/10 node_id=printer2
	// DBG got record message_id=generator1/10 node_id=printer1
	// DBG received ack message_id=generator1/10 node_id=generator1
	// INF stopping source connector component=SourceNode node_id=generator1
	// INF stopping source connector component=SourceNode node_id=generator2
	// INF stopping source node component=SourceNode node_id=generator1 record_position=10
	// INF stopping source node component=SourceNode node_id=generator2 record_position=10
	// DBG incoming messages channel closed component=SourceAckerNode node_id=generator1-acker
	// DBG incoming messages channel closed component=SourceAckerNode node_id=generator2-acker
	// DBG incoming messages channel closed component=ProcessorNode node_id=counter
	// DBG incoming messages channel closed component=DestinationNode node_id=printer1
	// DBG incoming messages channel closed component=DestinationNode node_id=printer2
	// DBG incoming messages channel closed component=DestinationAckerNode node_id=printer1-acker
	// DBG incoming messages channel closed component=DestinationAckerNode node_id=printer2-acker
	// INF counter node counted 20 messages
	// INF finished successfully
}

func newLogger() log.CtxLogger {
	w := zerolog.NewConsoleWriter()
	w.NoColor = true
	w.PartsExclude = []string{zerolog.TimestampFieldName}

	zlogger := zerolog.New(w)
	zlogger = zlogger.Level(zerolog.DebugLevel)
	logger := log.New(zlogger)
	logger = logger.CtxHook(ctxutil.MessageIDLogCtxHook{})

	return logger
}

func generatorSource(ctrl *gomock.Controller, logger log.CtxLogger, nodeID string, recordCount int, delay time.Duration) connector.Source {
	position := 0

	stop := make(chan struct{})
	source := connmock.NewSource(ctrl)
	source.EXPECT().ID().Return(nodeID).AnyTimes()
	source.EXPECT().Open(gomock.Any()).Return(nil).Times(1)
	source.EXPECT().Teardown(gomock.Any()).Return(nil).Times(1)
	source.EXPECT().Ack(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, p record.Position) error {
		logger.Debug(ctx).Str("node_id", nodeID).Msg("received ack")
		return nil
	}).Times(recordCount)
	source.EXPECT().Read(gomock.Any()).DoAndReturn(func(ctx context.Context) (record.Record, error) {
		time.Sleep(delay)

		if position == recordCount {
			// block until Stop is called
			<-stop
			return record.Record{}, plugin.ErrStreamNotOpen
		}

		position++
		return record.Record{
			Position: record.Position(strconv.Itoa(position)),
		}, nil
	}).MinTimes(recordCount + 1)
	source.EXPECT().Stop(gomock.Any()).DoAndReturn(func(context.Context) (record.Position, error) {
		close(stop)
		return record.Position(strconv.Itoa(position)), nil
	})
	source.EXPECT().Errors().Return(make(chan error))

	return source
}

func printerDestination(ctrl *gomock.Controller, logger log.CtxLogger, nodeID string) connector.Destination {
	var lastPosition record.Position
	rchan := make(chan record.Record, 1)
	destination := connmock.NewDestination(ctrl)
	destination.EXPECT().Open(gomock.Any()).Return(nil).Times(1)
	destination.EXPECT().Write(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, r record.Record) error {
		logger.Debug(ctx).
			Str("node_id", nodeID).
			Msg("got record")
		lastPosition = r.Position
		rchan <- r
		return nil
	}).AnyTimes()
	destination.EXPECT().Ack(gomock.Any()).DoAndReturn(func(ctx context.Context) (record.Position, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r, ok := <-rchan:
			if !ok {
				return nil, nil
			}
			return r.Position, nil
		}
	}).AnyTimes()
	destination.EXPECT().Stop(gomock.Any(), EqLazy(func() interface{} { return lastPosition })).Return(nil).Times(1)
	destination.EXPECT().Teardown(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		close(rchan)
		return nil
	}).Times(1)
	destination.EXPECT().Errors().Return(make(chan error))

	return destination
}

func counterProcessor(ctrl *gomock.Controller, count *int) processor.Interface {
	proc := procmock.NewProcessor(ctrl)
	proc.EXPECT().Process(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, r record.Record) (record.Record, error) {
		*count++
		return r, nil
	}).AnyTimes()
	return proc
}

func gomockCtrl(logger log.CtxLogger) *gomock.Controller {
	return gomock.NewController(gomockLogger(logger))
}

type gomockLogger log.CtxLogger

func (g gomockLogger) Errorf(format string, args ...interface{}) {
	g.Error().Msgf(format, args...)
}
func (g gomockLogger) Fatalf(format string, args ...interface{}) {
	g.Fatal().Msgf(format, args...)
}

func runNode(ctx context.Context, wg *sync.WaitGroup, n stream.Node) {
	defer wg.Done()
	err := n.Run(ctx)
	if err != nil {
		fmt.Printf("%s error: %v\n", n.ID(), err)
	}
}

func EqLazy(x func() interface{}) gomock.Matcher { return eqMatcherLazy{x} }

type eqMatcherLazy struct {
	x func() interface{}
}

func (e eqMatcherLazy) Matches(x interface{}) bool {
	return gomock.Eq(e.x()).Matches(x)
}
func (e eqMatcherLazy) String() string {
	return gomock.Eq(e.x()).String()
}
