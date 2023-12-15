package usecase

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/osmosis-labs/osmosis/v20/ingest/sqs/domain"
	"github.com/osmosis-labs/osmosis/v20/ingest/sqs/router/usecase/route"
)

type (
	RouterUseCaseImpl = routerUseCaseImpl

	QuoteImpl = quoteImpl

	CandidatePoolWrapper = candidatePoolWrapper
)

const (
	OsmoPrecisionMultiplier = osmoPrecisionMultiplier
	NoTotalValueLockedError = noTotalValueLockedError
)

func (r *Router) ValidateAndFilterRoutes(candidateRoutes [][]candidatePoolWrapper, tokenInDenom string) (route.CandidateRoutes, error) {
	return r.validateAndFilterRoutes(candidateRoutes, tokenInDenom)
}

func (r *routerUseCaseImpl) InitializeRouter() *Router {
	return r.initializeRouter()
}

func (r *routerUseCaseImpl) HandleRoutes(ctx context.Context, router *Router, tokenInDenom, tokenOutDenom string) (candidateRoutes route.CandidateRoutes, err error) {
	return r.handleCandidateRoutes(ctx, router, tokenInDenom, tokenOutDenom)
}

func (r *Router) EstimateAndRankSingleRouteQuote(routes []route.RouteImpl, tokenIn sdk.Coin) (domain.Quote, []RouteWithOutAmount, error) {
	return r.estimateAndRankSingleRouteQuote(routes, tokenIn)
}

// GetSortedPoolIDs returns the sorted pool IDs.
// The sorting is initialized in NewRouter() by preferredPoolIDs and TVL.
// Only used for tests.
func (r Router) GetSortedPoolIDs() []uint64 {
	sortedPoolIDs := make([]uint64, len(r.sortedPools))
	for i, pool := range r.sortedPools {
		sortedPoolIDs[i] = pool.GetId()
	}
	return sortedPoolIDs
}

func FilterDuplicatePoolIDRoutes(rankedRoutes []route.RouteImpl) []route.RouteImpl {
	return filterDuplicatePoolIDRoutes(rankedRoutes)
}

func ConvertRankedToCandidateRoutes(rankedRoutes []route.RouteImpl) route.CandidateRoutes {
	return convertRankedToCandidateRoutes(rankedRoutes)
}

func FormatRankedRouteCacheKey(tokenInDenom string, tokenOutDenom string, tokenIOrderOfMagnitude int) string {
	return formatRankedRouteCacheKey(tokenInDenom, tokenOutDenom, tokenIOrderOfMagnitude)
}
