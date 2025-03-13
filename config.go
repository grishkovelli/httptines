package httptines

// config represents the global configuration for the application
type config struct {
	// interval specifies the time in seconds between proxy checks
	interval int
	// pbuff specifies the buffer size for proxy processing
	pbuff int
	// sources contains a map of proxy source URLs grouped by schema
	sources proxySrc
	// statInterval specifies the interval in seconds for statistics updates
	statInterval int
	// strategy specifies the proxy selection strategy (minimal/auto)
	strategy string
	// testTarget specifies the URL used for testing proxy connections
	testTarget string
	// timeout specifies the request timeout in seconds
	timeout int
}
