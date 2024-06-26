// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package endpoints

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"strings"

	"github.com/GoogleCloudPlatform/ubbagent/metrics"
	"github.com/GoogleCloudPlatform/ubbagent/testlib"
	"github.com/GoogleCloudPlatform/ubbagent/util"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/servicecontrol/v1"
)

type recordingHandler struct {
	req         *http.Request
	body        []byte
	checkCount  int
	reportCount int
	t           testing.T
}

type mockNetError struct {
	temporary bool
	timeout   bool
}

func (e mockNetError) Error() string {
	return ""
}

func (e mockNetError) Temporary() bool {
	return e.temporary
}

func (e mockNetError) Timeout() bool {
	return e.timeout
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.req = r

	var err error
	h.body, err = ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}

	var respJson []byte
	if strings.Contains(r.RequestURI, ":check") {
		h.checkCount++
		req := &servicecontrol.CheckRequest{}
		err := json.Unmarshal(h.body, req)
		if err != nil {
			h.t.Fatalf("Unable to parse check request %+v", err)
		}

		resp := &servicecontrol.CheckResponse{}

		if req.Operation.OperationId == "billing-disabled" {
			resp.CheckErrors = []*servicecontrol.CheckError{{
				Code: "BILLING_DISABLED",
			}}
		}

		if req.Operation.OperationId == "check-unknown-error" {
			resp.CheckErrors = []*servicecontrol.CheckError{{
				Code: "UNKNOWN",
			}}
		}

		respJson, err = resp.MarshalJSON()
		if err != nil {
			panic(err)
		}
	}

	if strings.Contains(r.RequestURI, ":report") {
		h.reportCount++
		if h.checkCount == 0 {
			h.t.Fatalf("Check should be called before Report")
		}

		req := &servicecontrol.ReportRequest{}
		err := json.Unmarshal(h.body, req)
		if err != nil {
			h.t.Fatalf("Unable to parse report request %+v", err)
		}

		resp := &servicecontrol.ReportResponse{}
		if req.Operations[0].OperationId == "report-error" {
			resp.ReportErrors = []*servicecontrol.ReportError{{
				Status: &servicecontrol.Status{
					Message: "Unknown report error",
				},
			}}
		}

		respJson, err = resp.MarshalJSON()
		if err != nil {
			panic(err)
		}
	}

	w.Write(respJson)
}

func TestServiceControlEndpoint(t *testing.T) {
	handler := &recordingHandler{}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	svc, err := servicecontrol.New(http.DefaultClient)
	if err != nil {
		t.Fatalf("Error creating client: %+v", err)
	}

	// Point the service's path at our mock HTTP instance.
	svc.BasePath = ts.URL

	now := time.Now()
	mockClock := testlib.NewMockClock()
	mockClock.SetNow(now)
	ep := newServiceControlEndpoint("servicecontrol", "test-service.appspot.com", "unique-agent-id", "project_number:1234567", svc, mockClock)

	t.Run("Assert check is called first", func(t *testing.T) {
		// Test a single report write
		report1, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "report1",
			MetricReport: metrics.MetricReport{
				Name:      "int-metric1",
				StartTime: time.Unix(0, 0),
				EndTime:   time.Unix(1, 0),
				Value: metrics.MetricValue{
					Int64Value: util.NewInt64(10),
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}
		if report1.Id != "report1" {
			t.Fatalf("expected report ID to be 'report1', got: %v", report1.Id)
		}
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}

		if handler.reportCount != 1 {
			t.Fatalf("Report should have been called only once")
		}

		if handler.checkCount != 1 {
			t.Fatalf("Check should have been called only once")
		}

		mockClock.SetNow(now.Add(time.Second * 30))
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}

		if handler.reportCount != 2 {
			t.Fatalf("Report should have been called a second time")
		}

		if handler.checkCount != 1 {
			t.Fatalf("Check should have been called only once")
		}

		mockClock.SetNow(now.Add(time.Second * 61))
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}

		if handler.reportCount != 3 {
			t.Fatalf("Report should have been called a third time")
		}

		if handler.checkCount != 2 {
			t.Fatalf("Check should have been called a second time")
		}
	})

	t.Run("Report idempotence", func(t *testing.T) {
		// Test a single report write
		report1, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "report1",
			MetricReport: metrics.MetricReport{
				Name:      "int-metric1",
				StartTime: time.Unix(0, 0),
				EndTime:   time.Unix(1, 0),
				Value: metrics.MetricValue{
					Int64Value: util.NewInt64(10),
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}
		if report1.Id != "report1" {
			t.Fatalf("expected report ID to be 'report1', got: %v", report1.Id)
		}
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}

		// Test that a second send of the same report sends the same body
		body1, _ := ioutil.ReadAll(handler.req.Body)
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}
		body2, _ := ioutil.ReadAll(handler.req.Body)
		if !reflect.DeepEqual(body1, body2) {
			t.Fatal("two requests from same report were not equal")
		}
	})

	t.Run("Check error BILLING_DISABLED returns non-retriable error", func(t *testing.T) {
		ep.nextCheck = time.Now().Add(time.Minute * -1)

		// Test a single report write
		report, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "billing-disabled",
			MetricReport: metrics.MetricReport{
				Name:      "int-metric1",
				StartTime: time.Unix(0, 0),
				EndTime:   time.Unix(1, 0),
				Value: metrics.MetricValue{
					Int64Value: util.NewInt64(10),
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}

		err = ep.Send(report)
		if err == nil {
			t.Fatalf("expected error sending report")
		}

		checkErr := err.(*checkError)
		if checkErr.transient {
			t.Fatalf("expected billing disabled to not be a transient error")
		}

	})

	t.Run("Unknown check error returns retriable error", func(t *testing.T) {
		ep.nextCheck = time.Now().Add(time.Minute * -1)

		// Test a single report write
		report, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "check-unknown-error",
			MetricReport: metrics.MetricReport{
				Name:      "int-metric1",
				StartTime: time.Unix(0, 0),
				EndTime:   time.Unix(1, 0),
				Value: metrics.MetricValue{
					Int64Value: util.NewInt64(10),
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}

		err = ep.Send(report)
		if err == nil {
			t.Fatalf("expected error sending report")
		}

		checkErr := err.(*checkError)
		if !checkErr.transient {
			t.Fatalf("expected transient error")
		}

	})

	t.Run("Sent contents are correct", func(t *testing.T) {
		// Test a single report write
		report1, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "report1",
			MetricReport: metrics.MetricReport{
				Name:      "double-metric",
				StartTime: time.Unix(2, 0),
				EndTime:   time.Unix(3, 0),
				Value: metrics.MetricValue{
					DoubleValue: util.NewFloat64(20),
				},
				Labels: map[string]string{
					"foo": "bar",
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}
		if err := ep.Send(report1); err != nil {
			t.Fatalf("error sending report: %+v", err)
		}

		var doubleVal float64 = 20

		expectedOps := []*servicecontrol.Operation{
			{
				OperationName: "test-service.appspot.com/report",
				StartTime:     time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
				EndTime:       time.Unix(3, 0).UTC().Format(time.RFC3339Nano),
				ConsumerId:    "project_number:1234567",
				UserLabels: map[string]string{
					"goog-ubb-agent-id": "unique-agent-id",
					"foo":               "bar",
				},
				MetricValueSets: []*servicecontrol.MetricValueSet{
					{
						MetricName: "test-service.appspot.com/double-metric",
						MetricValues: []*servicecontrol.MetricValue{
							{
								StartTime:   time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
								EndTime:     time.Unix(3, 0).UTC().Format(time.RFC3339Nano),
								DoubleValue: &doubleVal,
							},
						},
					},
				},
			},
		}

		req := servicecontrol.ReportRequest{}
		if err := json.Unmarshal(handler.body, &req); err != nil {
			t.Fatalf("unmarshalling request: %+v", err)
		}

		// First we check to make sure each Operation has a unique ID, then zero the IDs
		// prior to comparing the rest of the values.
		usedIds := make(map[string]bool)
		for _, op := range req.Operations {
			if op.OperationId == "" {
				t.Fatal("found empty OperationId")
			}
			if usedIds[op.OperationId] {
				t.Fatalf("found reused OperationId: %v", op.OperationId)
			}
			usedIds[op.OperationId] = true
			op.OperationId = ""
		}

		if !reflect.DeepEqual(req.Operations, expectedOps) {
			t.Fatal("request operations didn't match expected")
		}
	})

	t.Run("ReportError returns transient error", func(t *testing.T) {
		// Test a single report write
		report, err := ep.BuildReport(metrics.StampedMetricReport{
			Id: "report-error",
			MetricReport: metrics.MetricReport{
				Name:      "int-metric1",
				StartTime: time.Unix(0, 0),
				EndTime:   time.Unix(1, 0),
				Value: metrics.MetricValue{
					Int64Value: util.NewInt64(10),
				},
			},
		})
		if err != nil {
			t.Fatalf("error building report: %+v", err)
		}

		err = ep.Send(report)
		if err == nil {
			t.Fatalf("expected error sending report")
		}

		if !ep.IsTransient(err) {
			t.Fatalf("expected transient error")
		}

		if !strings.Contains(err.Error(), "Unknown report error") {
			t.Fatalf("expected unknown report error")
		}
	})

	t.Run("IsTransient tests", func(t *testing.T) {
		cases := []struct {
			err       error
			transient bool
		}{
			{nil, false},
			{errors.New("foo"), true},
			{&googleapi.Error{Code: 404}, false},
			{&googleapi.Error{Code: 401}, false},
			{&googleapi.Error{Code: 500}, true},
			{&googleapi.Error{Code: 503}, true},
			{&googleapi.Error{Code: 599}, true},
			{&googleapi.Error{Code: 600}, false},
			{mockNetError{temporary: false, timeout: false}, false},
			{mockNetError{temporary: true, timeout: false}, true},
			{mockNetError{temporary: false, timeout: true}, true},
			{mockNetError{temporary: true, timeout: true}, true},
			{&checkError{err: errors.New("foo"), transient: true}, true},
			{&checkError{err: errors.New("foo"), transient: false}, false},
		}
		for _, c := range cases {
			if want, got := c.transient, ep.IsTransient(c.err); want != got {
				t.Fatalf("IsTransient for error %v: want=%v, got=%v", c.err, want, got)
			}
		}
	})

	// Test that Release returns successfully.
	ep.Release()
}
