package client

import (
	"net/http"

	"github.com/iotaledger/goshimmer/packages/jsonmodels"
)

const (
	routeAnchor = "anchor"
)

// IssueAnchor sends the given data (payload) by creating a message in the backend.
func (api *GoShimmerAPI) IssueAnchor(request *jsonmodels.Anchor) error {
	res := &jsonmodels.AnchorResponse{}
	if err := api.do(http.MethodPost, routeAnchor,
		request, res); err != nil {
		return err
	}

	return nil
}
