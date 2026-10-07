package confluence

// ActionErrors exposes actionErrors to the external tests.
var ActionErrors = actionErrors

// CloudForTest exposes cloud, which keeps the error IsCloud drops.
func CloudForTest(api *API) (bool, error) { return api.cloud() }
