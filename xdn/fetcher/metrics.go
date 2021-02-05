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

// Contains the metrics collected by the fetcher.

package fetcher

import (
	"github.com/xdn/go-xdn/metrics"
)

var (
	propAnnounceInMeter   = metrics.NewMeter("xdn/fetcher/prop/announces/in")
	propAnnounceOutTimer  = metrics.NewTimer("xdn/fetcher/prop/announces/out")
	propAnnounceDropMeter = metrics.NewMeter("xdn/fetcher/prop/announces/drop")
	propAnnounceDOSMeter  = metrics.NewMeter("xdn/fetcher/prop/announces/dos")

	propBroadcastInMeter   = metrics.NewMeter("xdn/fetcher/prop/broadcasts/in")
	propBroadcastOutTimer  = metrics.NewTimer("xdn/fetcher/prop/broadcasts/out")
	propBroadcastDropMeter = metrics.NewMeter("xdn/fetcher/prop/broadcasts/drop")
	propBroadcastDOSMeter  = metrics.NewMeter("xdn/fetcher/prop/broadcasts/dos")

	headerFetchMeter = metrics.NewMeter("xdn/fetcher/fetch/headers")
	bodyFetchMeter   = metrics.NewMeter("xdn/fetcher/fetch/bodies")

	headerFilterInMeter  = metrics.NewMeter("xdn/fetcher/filter/headers/in")
	headerFilterOutMeter = metrics.NewMeter("xdn/fetcher/filter/headers/out")
	bodyFilterInMeter    = metrics.NewMeter("xdn/fetcher/filter/bodies/in")
	bodyFilterOutMeter   = metrics.NewMeter("xdn/fetcher/filter/bodies/out")
)
