package hqlsp

import (
	"hq/internal/core"
	"hq/internal/hq"
	"hq/internal/hqprofile"
)

// HistoryServer embeds the canonical server and retains value-free projection
// evidence for diagnostics and tests. History failure never prevents startup.
type HistoryServer struct {
	*Server
	historyReport core.AcceptedHistoryReport
}

func NewWithAcceptedHistory(profile hqprofile.Profile) (*HistoryServer, error) {
	server, err := New(profile)
	if err != nil {
		return nil, err
	}
	wrapped := &HistoryServer{Server: server}
	if server.recall == nil || profile.AcceptedPath == "" {
		return wrapped, nil
	}
	rows, readReport, readErr := (fileAcceptedHistoryReader{}).Read(profile.AcceptedPath)
	if readErr != nil || readReport.Fatal {
		wrapped.historyReport = mergeHistoryReports(readReport, historyReadErrorReport(readErr))
		return wrapped, nil
	}
	combined, projectionReport := hq.AttachAcceptedHistory(server.world, server.recall, rows)
	wrapped.historyReport = mergeHistoryReports(readReport, projectionReport)
	if !wrapped.historyReport.Fatal && combined != nil {
		server.recall = combined
	}
	return wrapped, nil
}

func (server *HistoryServer) HistoryReport() core.AcceptedHistoryReport {
	if server == nil {
		return core.AcceptedHistoryReport{}
	}
	return server.historyReport
}
