package chat

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderIPPolicy(t *testing.T) {
	publicPolicy := providerEgressPolicyFor(ProviderOpenAI, ProviderEgressConfig{})
	ollamaPolicy := providerEgressPolicyFor(ProviderOllama, ProviderEgressConfig{})
	privateOllamaPolicy := providerEgressPolicyFor(ProviderOllama, ProviderEgressConfig{AllowPrivateOllama: true})

	assert.False(t, providerIPAllowed(publicPolicy, net.ParseIP("127.0.0.1")))
	assert.False(t, providerIPAllowed(publicPolicy, net.ParseIP("10.0.0.1")))
	assert.True(t, providerIPAllowed(publicPolicy, net.ParseIP("203.0.113.10")))

	assert.True(t, providerIPAllowed(ollamaPolicy, net.ParseIP("127.0.0.1")))
	assert.False(t, providerIPAllowed(ollamaPolicy, net.ParseIP("10.0.0.1")))
	assert.True(t, providerIPAllowed(privateOllamaPolicy, net.ParseIP("10.0.0.1")))
	assert.False(t, providerIPAllowed(ollamaPolicy, net.ParseIP("169.254.169.254")))
	assert.False(t, providerIPAllowed(privateOllamaPolicy, net.ParseIP("169.254.169.254")))
	assert.False(t, providerIPAllowed(ollamaPolicy, net.ParseIP("198.18.0.1")))
}

func TestProviderHTTPClientRejectsPublicProviderLoopback(t *testing.T) {
	client := newProviderHTTPClient(providerEgressPolicyFor(ProviderOpenAI, ProviderEgressConfig{}))

	response, err := client.Get("https://127.0.0.1:4444")
	if response != nil {
		response.Body.Close()
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disallowed internal address")
}

func TestProviderHTTPClientRejectsPrivateOllamaByDefault(t *testing.T) {
	client := newProviderHTTPClient(providerEgressPolicyFor(ProviderOllama, ProviderEgressConfig{}))

	response, err := client.Get("http://10.0.0.1:11434")
	if response != nil {
		response.Body.Close()
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disallowed internal address")
}

func TestProviderHTTPClientDoesNotFollowRedirects(t *testing.T) {
	redirectFollowed := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectFollowed = true
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	client := newProviderHTTPClient(providerEgressPolicyFor(ProviderOllama, ProviderEgressConfig{}))
	response, err := client.Get(redirect.URL)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusFound, response.StatusCode)
	assert.False(t, redirectFollowed)
}
