package httpclient

// SetSubscriberBuffer sets how far a terminal subscriber may fall behind and
// returns the previous value; tests shrink it to make a client lag.
func SetSubscriberBuffer(n int) int {
	old := subscriberBuffer
	subscriberBuffer = n
	return old
}
