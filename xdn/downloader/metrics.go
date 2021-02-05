// Copyright 2015 The go-xdn Authors
// This file is part of the go-xdn library.
//
// The go-xdn library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-xdn library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-xdn library. If not, see <http://www.gnu.org/licenses/>.

// Contains the metrics collected by the downloader.

package downloader

import (
	"github.com/xdn/go-xdn/metrics"
)

var (
	headerInMeter      = metrics.NewMeter("xdn/downloader/headers/in")
	headerReqTimer     = metrics.NewTimer("xdn/downloader/headers/req")
	headerDropMeter    = metrics.NewMeter("xdn/downloader/headers/drop")
	headerTimeoutMeter = metrics.NewMeter("xdn/downloader/headers/timeout")

	bodyInMeter      = metrics.NewMeter("xdn/downloader/bodies/in")
	bodyReqTimer     = metrics.NewTimer("xdn/downloader/bodies/req")
	bodyDropMeter    = metrics.NewMeter("xdn/downloader/bodies/drop")
	bodyTimeoutMeter = metrics.NewMeter("xdn/downloader/bodies/timeout")

	receiptInMeter      = metrics.NewMeter("xdn/downloader/receipts/in")
	receiptReqTimer     = metrics.NewTimer("xdn/downloader/receipts/req")
	receiptDropMeter    = metrics.NewMeter("xdn/downloader/receipts/drop")
	receiptTimeoutMeter = metrics.NewMeter("xdn/downloader/receipts/timeout")

	stateInMeter   = metrics.NewMeter("xdn/downloader/states/in")
	stateDropMeter = metrics.NewMeter("xdn/downloader/states/drop")
)
