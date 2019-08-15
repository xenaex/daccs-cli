package clients

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"github.com/shopspring/decimal"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh/terminal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	macaroon "gopkg.in/macaroon.v2"
)

const (
	defaultGRPCTimeout = 5 * time.Second
	defaultWaitUnlock  = 5 * time.Second
	defaultCSVDelay    = 288 // ~48 hours

	recreateAfterUnlockAttemptsCount = 5
	recreateAfterUnlockInterval      = time.Second
)

// ChannelStatus descriptor
type ChannelStatus struct {
	ID            uint64          `json:"id,omitempty"`
	Node          string          `json:"node"`
	ChannelPoint  string          `json:"channel_point"`
	Status        string          `json:"status"`
	Capacity      decimal.Decimal `json:"capacity"`
	LocalBalance  decimal.Decimal `json:"local_balance"`
	RemoteBalance decimal.Decimal `json:"remote_balance"`
	ClosingTxid   string          `json:"closing_txid,omitempty"`
}

// OpenChannelResult description
type OpenChannelResult struct {
	ChannelStatus
	Error error
}

// Payment description
type Payment struct {
	Node      string          `json:"node"`
	Timestamp time.Time       `json:"timestamp"`
	Amount    decimal.Decimal `json:"amount"`
}

// LndClient interface
type LndClient interface {
	// Unlock local node wallet to bring it online
	Unlock(password string) error
	// Status of the local LND node
	Status() (*lnrpc.GetInfoResponse, error)
	// NodePubKey for local node
	NodePubKey() (string, error)
	// Peers the local node connected to
	Peers() ([]string, error)
	// Connect local node to remote LND node
	Connect(address string) error
	// Disconnect local node from remote LND node
	Disconnect(address string) error
	// Balance in BTC available on the local LND wallet
	Balance() (decimal.Decimal, error)
	// FundingAddress for the local LND wallet
	FundingAddress() (string, error)
	// OpenChannel to specified node and commit specified amount to it
	OpenChannel(address string, amount decimal.Decimal, out chan *OpenChannelResult) error
	// Channels list
	Channels() ([]*ChannelStatus, error)
	// ActiveChannels list
	ActiveChannels() ([]*ChannelStatus, error)
	// CloseChannel with specified channel point
	CloseChannel(chanID uint64, chanPoint string) (*ChannelStatus, error)
	// SendPayment by specified payment request on specified amount
	SendPayment(paymentReq string, amount decimal.Decimal) error
	// Payments list
	Payments(offset, limit int) ([]Payment, error)
	// Close gRPC connection
	Close() error
}

// lndClient implementation
type lndClient struct {
	connection     *grpc.ClientConn
	walletUnlocker lnrpc.WalletUnlockerClient
	client         lnrpc.LightningClient
}

// NewLndClient constructor
func NewLndClient(c *cli.Context, unlocked bool) (LndClient, error) {
	// Get and check parameters
	lndHost := c.GlobalString("lnd-host")
	if lndHost == "" {
		return nil, errors.New("lnd-host is not specified")
	}
	tlsCertPath := c.GlobalString("lnd-tls-cert")
	if tlsCertPath == "" {
		return nil, errors.New("lnd-tls-cert is not specified")
	}
	macaroonPath := c.GlobalString("lnd-macaroon")
	if macaroonPath == "" {
		return nil, errors.New("lnd-macaroon is not specified")
	}

	// Prepare gRPC connection credentials and options
	tlsCreds, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, fmt.Errorf("Error %s on reading TLS certificate %s", err, tlsCertPath)
	}
	macBytes, err := ioutil.ReadFile(macaroonPath)
	if err != nil {
		return nil, fmt.Errorf("Error %s on reading macaroon %s", err, macaroonPath)
	}
	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macBytes); err != nil {
		return nil, fmt.Errorf("Error %s on parsing macaroon %s", err, macaroonPath)
	}
	macCred := macaroons.NewMacaroonCredential(mac)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
		grpc.WithPerRPCCredentials(macCred),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50 * 1024 * 1024)),
	}

	conn, err := grpc.Dial(lndHost, opts...)
	if err != nil {
		return nil, fmt.Errorf("Error %s on connecting to lnd %s", err, lndHost)
	}

	walletUnlocker := lnrpc.NewWalletUnlockerClient(conn)
	client := lnrpc.NewLightningClient(conn)

	// Check node status and unlock if needed
	if unlocked {
		ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
		defer cancel()
		_, err = client.GetInfo(ctx, &lnrpc.GetInfoRequest{})
		if err != nil {
			s, ok := status.FromError(err)
			if !ok {
				return nil, fmt.Errorf("Error %s on connecting to lnd %s", err, lndHost)
			}
			if s.Code() != codes.Unimplemented {
				return nil, fmt.Errorf("Error %s on connecting to lnd %s", err, lndHost)
			}

			fmt.Println("Local LND node is locked")
			fmt.Printf("Input unlock password: ")
			pwd, err := terminal.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return nil, err
			}
			fmt.Println()

			ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
			defer cancel()

			_, err = walletUnlocker.UnlockWallet(ctx, &lnrpc.UnlockWalletRequest{WalletPassword: ([]byte)(pwd)})
			if err != nil {
				return nil, fmt.Errorf("Error %s on unlocking lnd node", err)
			}

			// Wait until lnd rpc server is ready, recreate client and test with GetInfo()
			// Otherwise rpc client will reply "Unimplemented" for every request
			for i := 0; i < recreateAfterUnlockAttemptsCount; i++ {
				time.Sleep(recreateAfterUnlockInterval)
				conn.Close()
				conn, err = grpc.Dial(lndHost, opts...)
				if err != nil {
					return nil, fmt.Errorf("Error %s on connecting to lnd %s", err, lndHost)
				}
				client = lnrpc.NewLightningClient(conn)
				ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
				defer cancel()
				_, err = client.GetInfo(ctx, &lnrpc.GetInfoRequest{})
				if err == nil {
					break
				}
			}
			if err != nil {
				return nil, err
			}
		}
	}

	return &lndClient{
		connection:     conn,
		walletUnlocker: walletUnlocker,
		client:         client,
	}, nil
}

// Unlock local node wallet to bring it online
func (c *lndClient) Unlock(password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	_, err := c.walletUnlocker.UnlockWallet(ctx, &lnrpc.UnlockWalletRequest{WalletPassword: ([]byte)(password)})
	return err
}

// Status of the local LND node
func (c *lndClient) Status() (*lnrpc.GetInfoResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	return c.client.GetInfo(ctx, &lnrpc.GetInfoRequest{})
}

// NodePubKey for local node
func (c *lndClient) NodePubKey() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	res, err := c.client.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return "", err
	}
	return res.IdentityPubkey, nil
}

// Peers the local node connected to
func (c *lndClient) Peers() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	peers, err := c.client.ListPeers(ctx, &lnrpc.ListPeersRequest{})
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, p := range peers.Peers {
		res = append(res, fmt.Sprintf("%s@%s", p.PubKey, p.Address))
	}
	return res, nil
}

// Connect local node to remote LND node
func (c *lndClient) Connect(address string) error {
	addrParts := strings.Split(address, "@")
	if len(addrParts) != 2 || addrParts[0] == "" || addrParts[1] == "" {
		return fmt.Errorf("Invalid address format: %s", address)
	}
	pubKey := addrParts[0]
	host := addrParts[1]
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	_, err := c.client.ConnectPeer(ctx, &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: pubKey,
			Host:   host,
		},
	})
	return err
}

// Disconnect local node from remote LND node
func (c *lndClient) Disconnect(address string) error {
	addrParts := strings.Split(address, "@")
	if len(addrParts) != 2 || addrParts[0] == "" || addrParts[1] == "" {
		return fmt.Errorf("Invalid address format: %s", address)
	}
	pubKey := addrParts[0]
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	_, err := c.client.DisconnectPeer(ctx, &lnrpc.DisconnectPeerRequest{
		PubKey: pubKey,
	})
	return err
}

// Balance in BTC available on the local LND wallet
func (c *lndClient) Balance() (decimal.Decimal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	bal, err := c.client.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return decimal.Zero, err
	}
	return satoshiToBTC(bal.ConfirmedBalance), nil
}

// FundingAddress for the local LND wallet
func (c *lndClient) FundingAddress() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	addr, err := c.client.NewAddress(ctx, &lnrpc.NewAddressRequest{})
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}

// OpenChannel to specified node and commit specified amount to it
func (c *lndClient) OpenChannel(address string, amount decimal.Decimal, out chan *OpenChannelResult) error {
	addrParts := strings.Split(address, "@")
	if len(addrParts) != 2 || addrParts[0] == "" || addrParts[1] == "" {
		return fmt.Errorf("Invalid address format: %s", address)
	}
	pubKey, err := hex.DecodeString(addrParts[0])
	if err != nil {
		return err
	}
	req := &lnrpc.OpenChannelRequest{
		RemoteCsvDelay:     defaultCSVDelay,
		NodePubkey:         pubKey,
		LocalFundingAmount: btcToSatoshi(amount),
		Private:            true,
	}
	stream, err := c.client.OpenChannel(context.Background(), req)
	if err != nil {
		return err
	}
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				out <- &OpenChannelResult{ChannelStatus: ChannelStatus{Node: address}, Error: err}
				return
			}

			switch update := resp.Update.(type) {
			case *lnrpc.OpenStatusUpdate_ChanPending:
				txid, err := chainhash.NewHash(update.ChanPending.Txid)
				if err != nil {
					out <- &OpenChannelResult{ChannelStatus: ChannelStatus{Node: address}, Error: err}
					return
				}
				out <- &OpenChannelResult{
					ChannelStatus: ChannelStatus{
						Node:         addrParts[0],
						ChannelPoint: fmt.Sprintf("%s:%d", txid.String(), update.ChanPending.OutputIndex),
						Status:       "pending_open",
						Capacity:     amount,
					},
				}
				return
			}
		}
	}()
	return nil
}

// Channels list
func (c *lndClient) Channels() ([]*ChannelStatus, error) {
	res := []*ChannelStatus{}
	// Pending channels
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	pending, err := c.client.PendingChannels(ctx, &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return nil, err
	}
	for _, c := range pending.PendingOpenChannels {
		res = append(res, pendingChannelStatus(c.Channel, "pending_open"))
	}
	for _, c := range pending.PendingClosingChannels {
		res = append(res, pendingChannelStatus(c.Channel, "pending_closing"))
	}
	for _, c := range pending.PendingForceClosingChannels {
		res = append(res, pendingChannelStatus(c.Channel, "pending_force_closing"))
	}
	for _, c := range pending.WaitingCloseChannels {
		res = append(res, pendingChannelStatus(c.Channel, "waiting_close"))
	}
	// Active channels
	ctx, cancel = context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	active, err := c.client.ListChannels(ctx, &lnrpc.ListChannelsRequest{ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	for _, c := range active.Channels {
		res = append(res, channelStatus(c, "active"))
	}
	// Inactive channels
	ctx, cancel = context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	inactive, err := c.client.ListChannels(ctx, &lnrpc.ListChannelsRequest{InactiveOnly: true})
	if err != nil {
		return nil, err
	}
	for _, c := range inactive.Channels {
		res = append(res, channelStatus(c, "inactive"))
	}
	return res, nil
}

// ActiveChannels list
func (c *lndClient) ActiveChannels() ([]*ChannelStatus, error) {
	res := []*ChannelStatus{}
	// Active channels
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	active, err := c.client.ListChannels(ctx, &lnrpc.ListChannelsRequest{ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	for _, c := range active.Channels {
		res = append(res, channelStatus(c, "active"))
	}
	return res, nil
}

// CloseChannel with specified channel point
func (c *lndClient) CloseChannel(chanID uint64, chanPoint string) (*ChannelStatus, error) {
	// Find channel
	list, err := c.Channels()
	if err != nil {
		return nil, err
	}
	var channel *ChannelStatus
	for _, ch := range list {
		if (chanID != 0 && ch.ID == chanID) || (chanPoint != "" && ch.ChannelPoint == chanPoint) {
			channel = ch
			break
		}
	}
	if channel == nil {
		return nil, fmt.Errorf("channel not found")
	}

	// Parse channel point
	channelPoint := &lnrpc.ChannelPoint{}
	chanPointParts := strings.Split(channel.ChannelPoint, ":")
	if len(chanPointParts) != 2 {
		return nil, errors.New("invalid ChannelPoint format")
	}
	channelPoint.FundingTxid = &lnrpc.ChannelPoint_FundingTxidStr{
		FundingTxidStr: chanPointParts[0],
	}
	index, err := strconv.ParseUint(chanPointParts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("inable to decode output index: %v", err)
	}
	channelPoint.OutputIndex = uint32(index)

	// Close channel
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	ch, err := c.client.CloseChannel(ctx, &lnrpc.CloseChannelRequest{ChannelPoint: channelPoint})
	for {
		m, err := ch.Recv()
		if err != nil {
			return nil, err
		}
		switch x := m.Update.(type) {
		case *lnrpc.CloseStatusUpdate_ClosePending:
			closingHash := x.ClosePending.Txid
			txid, err := chainhash.NewHash(closingHash)
			if err != nil {
				return nil, err
			}
			channel.ClosingTxid = txid.String()
			channel.Status = "waiting_close"
			return channel, nil
		}
	}
	return channel, nil
}

// SendPayment by specified payment request on specified amount
func (c *lndClient) SendPayment(paymentReq string, amount decimal.Decimal) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	resp, err := c.client.SendPaymentSync(ctx, &lnrpc.SendRequest{
		PaymentRequest: paymentReq,
		Amt:            btcToSatoshi(amount),
	})
	if err != nil {
		return err
	}
	if resp.PaymentError != "" {
		return errors.New(resp.PaymentError)
	}
	return nil
}

// Payments list
func (c *lndClient) Payments(offset, limit int) ([]Payment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultGRPCTimeout)
	defer cancel()
	resp, err := c.client.ListPayments(ctx, &lnrpc.ListPaymentsRequest{})
	if err != nil {
		return nil, err
	}
	sort.Slice(resp.Payments, func(i, j int) bool { return resp.Payments[i].CreationDate > resp.Payments[j].CreationDate })
	res := []Payment{}
	if offset >= len(resp.Payments) {
		return res, nil
	}
	last := offset + limit
	if last > len(resp.Payments) {
		last = len(resp.Payments)
	}
	for _, p := range resp.Payments[offset:last] {
		res = append(res, Payment{
			Node:      p.Path[0],
			Timestamp: time.Unix(p.CreationDate, 0),
			Amount:    satoshiToBTC(p.ValueSat),
		})
	}
	return res, nil
}

// Close gRPC connection
func (c *lndClient) Close() error {
	if c.connection != nil {
		conn := c.connection
		c.connection = nil
		return conn.Close()
	}
	return nil
}

func satoshiToBTC(sat int64) decimal.Decimal {
	return decimal.New(sat, -8)
}

func btcToSatoshi(btc decimal.Decimal) int64 {
	return btc.Mul(decimal.New(1, 8)).IntPart()
}

func channelStatus(c *lnrpc.Channel, status string) *ChannelStatus {
	return &ChannelStatus{
		ID:            c.ChanId,
		Node:          c.RemotePubkey,
		ChannelPoint:  c.ChannelPoint,
		Capacity:      satoshiToBTC(c.Capacity),
		LocalBalance:  satoshiToBTC(c.LocalBalance),
		RemoteBalance: satoshiToBTC(c.RemoteBalance),
		Status:        status,
	}
}

func pendingChannelStatus(c *lnrpc.PendingChannelsResponse_PendingChannel, status string) *ChannelStatus {
	return &ChannelStatus{
		Node:          c.RemoteNodePub,
		ChannelPoint:  c.ChannelPoint,
		Capacity:      satoshiToBTC(c.Capacity),
		LocalBalance:  satoshiToBTC(c.LocalBalance),
		RemoteBalance: satoshiToBTC(c.RemoteBalance),
		Status:        status,
	}
}
