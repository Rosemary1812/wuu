package appserver

import (
	"context"
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/modelcatalog"
)

func (s *Server) handleConfigModelCatalogRefresh(ctx context.Context, req Request) error {
	if s == nil || s.rt == nil {
		return s.writeResponse(req.ID, nil, errors.New("runtime session is required"))
	}
	cachePath := strings.TrimSpace(s.modelCatalogCachePath)
	if cachePath == "" {
		return s.writeResponse(req.ID, nil, errors.New("model catalog cache path is unavailable"))
	}
	counts, err := modelcatalog.Refresh(ctx, modelcatalog.RefreshOptions{
		URL:       s.modelCatalogURL,
		CachePath: cachePath,
		Client:    s.modelCatalogHTTPClient,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providers := s.providerSummaries()
	if providers == nil {
		providers = []ProviderSummary{}
	}
	return s.writeResponse(req.ID, ConfigModelCatalogRefreshResult{
		ProviderCount: counts.Providers,
		ModelCount:    counts.Models,
		Providers:     providers,
	}, nil)
}
