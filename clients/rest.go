package clients

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"github.com/urfave/cli"
)

const (
	defaultRequestTimeout = 30 * time.Second
)

// RestClient interface for Xena dAccs API
type RestClient interface {
	// RegisterNode registers local lnd node in assoiciation with Xena user
	RegisterNode(pubKey string) error
	// RemoteAddresses of Xena lnd nodes to connect to
	RemoteAddresses() ([]string, error)
	// IssueInvoices to pay via available channels
	IssueInvoices(accountID int64, chanPoints []string) ([]Invoice, error)
	// Limits returns daccs limits
	Limits() (*Limits, error)
}

// restClient implementation
type restClient struct {
	client    *http.Client
	baseURL   *url.URL
	apiKey    string
	apiSecret *ecdsa.PrivateKey
}

// NewRestClient constructor
func NewRestClient(c *cli.Context) (RestClient, error) {
	// Get and check parameters
	apiURL := c.GlobalString("api-url")
	if apiURL == "" {
		return nil, errors.New("api-url is not specified")
	}
	apiKey := c.GlobalString("api-key")
	if apiKey == "" {
		return nil, errors.New("api-key is not specified")
	}
	apiSecret := c.GlobalString("api-secret")
	if apiSecret == "" {
		return nil, errors.New("api-secret is not specified")
	}

	baseURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("Error %s on parsing api-url", err)
	}

	privKeyData, err := hex.DecodeString(apiSecret)
	if err != nil {
		return nil, fmt.Errorf("Error %s on decoding api-secret", err)
	}
	privKey, err := x509.ParseECPrivateKey(privKeyData)
	if err != nil {
		return nil, fmt.Errorf("Error %s on parsing api-secret", err)
	}

	return &restClient{
		client: &http.Client{
			Timeout: defaultRequestTimeout,
		},
		baseURL:   baseURL,
		apiKey:    apiKey,
		apiSecret: privKey,
	}, nil
}

// RegisterNode registers local lnd node in assoiciation with Xena user
func (c *restClient) RegisterNode(pubKey string) error {
	req := &addPubKeyRequest{
		PubKey: pubKey,
	}
	_, err := c.call("pubkey", "POST", req)
	if err != nil {
		return err
	}
	return nil
}

// RemoteAddresses of Xena lnd nodes to connect to
func (c *restClient) RemoteAddresses() ([]string, error) {
	respData, err := c.call("addresses", "GET", nil)
	if err != nil {
		return nil, err
	}
	var resp []address
	err = json.Unmarshal(respData, &resp)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, a := range resp {
		res = append(res, a.Address)
	}
	return res, nil
}

// IssueInvoices to pay via specified channels
func (c *restClient) IssueInvoices(accountID int64, chanPoints []string) ([]Invoice, error) {
	req := invoiceRequest{
		ExternalID: time.Now().UTC().String(),
		ChanPoints: chanPoints,
	}
	respData, err := c.call(fmt.Sprintf("accounts/%d/invoices", accountID), "POST", &req)
	if err != nil {
		return nil, err
	}
	var resp []Invoice
	err = json.Unmarshal(respData, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Limits returns daccs limits
func (c *restClient) Limits() (*Limits, error) {
	respData, err := c.call("limits", "GET", nil)
	if err != nil {
		return nil, err
	}
	resp := &Limits{}
	err = json.Unmarshal(respData, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// call Xena dAccs API with authentication
func (c *restClient) call(path, method string, request interface{}) ([]byte, error) {
	// Request URL
	urlPath, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("%s on parsing request path %s", err, path)
	}

	// Prepare request body if any
	reqBody := &bytes.Buffer{}
	if request != nil {
		bodyData, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("%s on marshaling request body", err)
		}
		reqBody = bytes.NewBuffer(bodyData)
	}

	// Prepare HTTP request
	req, err := http.NewRequest(method, c.baseURL.ResolveReference(urlPath).String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("%s on creating HTTP request", err)
	}

	// Prepare auth credentials & headers
	nonce := time.Now().UnixNano()
	payload := fmt.Sprintf("AUTH%d", nonce)
	digest := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, c.apiSecret, digest[:])
	signature := append(r.Bytes(), s.Bytes()...)
	sigHex := hex.EncodeToString(signature)
	req.Header.Add("X-AUTH-API-KEY", c.apiKey)
	req.Header.Add("X-AUTH-API-PAYLOAD", payload)
	req.Header.Add("X-AUTH-API-SIGNATURE", sigHex)
	req.Header.Add("X-AUTH-API-NONCE", strconv.FormatInt(nonce, 10))

	// Issue request and obtain response
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s on performing %s request to %s", err, req.Method, req.URL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New(resp.Status)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s on reading response", err)
	}

	// Check response for error
	var errResp errorResponse
	err = json.Unmarshal(data, &errResp)
	if err == nil && errResp.Error != "" {
		return nil, errors.New(errResp.Error)
	}

	return data, nil
}

// addPubKeyRequest message
type addPubKeyRequest struct {
	PubKey string `json:"pubKey"`
}

// pubKeyInfo message
type pubKeyInfo struct {
	ID     uint32 `json:"id"`
	PubKey string `json:"pubKey"`
	Exists bool   `json:"exists"`
}

// address message
type address struct {
	Address string `json:"address"`
}

// invoiceRequest message
type invoiceRequest struct {
	ExternalID string   `json:"externalId"`
	ChanPoints []string `json:"chanPoints"`
}

// Invoice message
type Invoice struct {
	NodeID         string `json:"nodeId"`
	PaymentRequest string `json:"paymentRequest"`
	ChanPoint      string `json:"chanPoint"`
}

// Limits message
type Limits struct {
	MinChannelCapacity decimal.Decimal `json:"minChannelCapacity"`
	MinPaymentAmount   decimal.Decimal `json:"minPaymentAmount"`
}

// error message
type errorResponse struct {
	Error string `json:"error"`
}
