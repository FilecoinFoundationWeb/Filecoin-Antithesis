package foc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultCurioURL    = "http://curio:80"
	pollInterval       = 4 * time.Second
	pollTimeout        = 5 * time.Minute
	httpRequestTimeout = 5 * time.Minute
)

// DataSetInfo represents the response from GET /pdp/data-sets/{id}.
type DataSetInfo struct {
	ID                 int         `json:"id"`
	NextChallengeEpoch int64       `json:"nextChallengeEpoch"`
	Pieces             []PieceInfo `json:"pieces"`
}

// PieceInfo represents a piece within a dataset.
type PieceInfo struct {
	PieceID  int    `json:"pieceId"`
	PieceCID string `json:"pieceCid"`
}

// DataSetCreationStatus mirrors the Curio status response.
type DataSetCreationStatus struct {
	CreateMessageHash string `json:"createMessageHash"`
	DataSetCreated    bool   `json:"dataSetCreated"`
	Service           string `json:"service"`
	TxStatus          string `json:"txStatus"`
	OK                *bool  `json:"ok"`
	DataSetID         *int   `json:"dataSetId,omitempty"`
}

// PieceAdditionStatus mirrors the Curio piece-addition status response.
type PieceAdditionStatus struct {
	TxHash            string `json:"txHash"`
	TxStatus          string `json:"txStatus"`
	DataSetID         int    `json:"dataSetId"`
	PieceCount        int    `json:"pieceCount"`
	AddMessageOK      *bool  `json:"addMessageOk"`
	ConfirmedPieceIDs []int  `json:"confirmedPieceIds,omitempty"`
}

// CurioBaseURL returns the Curio PDP API base URL from env or default.
func CurioBaseURL() string {
	if v := os.Getenv("CURIO_PDP_URL"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return defaultCurioURL
}

func curioHTTPClient() *http.Client {
	return &http.Client{Timeout: httpRequestTimeout}
}

// PingCurio checks if Curio's PDP API is reachable.
func PingCurio(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", CurioBaseURL()+"/pdp/ping", nil)
	if err != nil {
		return false
	}
	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// UploadPiece uploads raw piece data to Curio via the 3-step PDP upload flow.
// Returns the pieceCID string that Curio confirms.
func UploadPiece(ctx context.Context, data []byte, pieceCID string) error {
	base := CurioBaseURL()
	client := curioHTTPClient()

	// Step 1: Create upload session
	createReq, err := http.NewRequestWithContext(ctx, "POST", base+"/pdp/piece/uploads", nil)
	if err != nil {
		return fmt.Errorf("create session request: %w", err)
	}
	createResp, err := client.Do(createReq)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("create session: status %d: %s", createResp.StatusCode, body)
	}

	location := createResp.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("create session: missing Location header")
	}

	uuidRe := regexp.MustCompile(`/pdp/piece/uploads/([a-fA-F0-9-]+)`)
	matches := uuidRe.FindStringSubmatch(location)
	if len(matches) < 2 {
		return fmt.Errorf("create session: invalid Location: %s", location)
	}
	uploadUUID := matches[1]

	// Step 2: Upload raw data
	uploadReq, err := http.NewRequestWithContext(ctx, "PUT", base+"/pdp/piece/uploads/"+uploadUUID, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadReq.ContentLength = int64(len(data))

	uploadResp, err := client.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(uploadResp.Body)
		return fmt.Errorf("upload: status %d: %s", uploadResp.StatusCode, body)
	}

	// Step 3: Finalize with pieceCID
	finalizeBody, err := json.Marshal(map[string]string{"pieceCid": pieceCID})
	if err != nil {
		return fmt.Errorf("marshal finalize: %w", err)
	}

	finalizeReq, err := http.NewRequestWithContext(ctx, "POST", base+"/pdp/piece/uploads/"+uploadUUID, bytes.NewReader(finalizeBody))
	if err != nil {
		return fmt.Errorf("finalize request: %w", err)
	}
	finalizeReq.Header.Set("Content-Type", "application/json")

	finalizeResp, err := client.Do(finalizeReq)
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	defer finalizeResp.Body.Close()

	if finalizeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeResp.Body)
		return fmt.Errorf("finalize: status %d: %s", finalizeResp.StatusCode, body)
	}

	return nil
}

// FindPiece checks if a piece is indexed in Curio.
func FindPiece(ctx context.Context, pieceCID string) error {
	params := url.Values{}
	params.Set("pieceCid", pieceCID)

	req, err := http.NewRequestWithContext(ctx, "GET", CurioBaseURL()+"/pdp/piece?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("find piece request: %w", err)
	}

	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("find piece: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("piece not found: %s", pieceCID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("find piece: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// WaitForPiece polls until a piece is indexed in Curio.
func WaitForPiece(ctx context.Context, pieceCID string) error {
	return poll(ctx, pollInterval, pollTimeout, func() (bool, error) {
		err := FindPiece(ctx, pieceCID)
		if err != nil {
			if strings.Contains(err.Error(), "piece not found") {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

// CreateDataSetHTTP creates a dataset via Curio's HTTP API.
// Returns the txHash from the Location header.
func CreateDataSetHTTP(ctx context.Context, recordKeeper, extraData string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"recordKeeper": recordKeeper,
		"extraData":    extraData,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", CurioBaseURL()+"/pdp/data-sets", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("create dataset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create dataset: status %d: %s", resp.StatusCode, respBody)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("create dataset: missing Location header")
	}

	parts := strings.Split(location, "/")
	txHash := parts[len(parts)-1]
	if !strings.HasPrefix(txHash, "0x") {
		return "", fmt.Errorf("create dataset: invalid txHash in Location: %s", location)
	}
	return txHash, nil
}

// WaitForDataSetCreation polls until a dataset creation tx is confirmed.
func WaitForDataSetCreation(ctx context.Context, txHash string) (int, error) {
	var dataSetID int
	err := poll(ctx, pollInterval, pollTimeout, func() (bool, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", CurioBaseURL()+"/pdp/data-sets/created/"+txHash, nil)
		if err != nil {
			return false, err
		}
		resp, err := curioHTTPClient().Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("status %d: %s", resp.StatusCode, body)
		}

		var status DataSetCreationStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return false, err
		}
		if status.DataSetCreated && status.DataSetID != nil {
			dataSetID = *status.DataSetID
			return true, nil
		}
		return false, nil
	})
	return dataSetID, err
}

// AddPiecesHTTP adds pieces to a dataset via Curio's HTTP API.
// Returns the txHash for tracking.
func AddPiecesHTTP(ctx context.Context, dataSetID int, pieceCIDs []string, extraData string) (string, error) {
	type subPiece struct {
		SubPieceCID string `json:"subPieceCid"`
	}
	type piece struct {
		PieceCID  string     `json:"pieceCid"`
		SubPieces []subPiece `json:"subPieces"`
	}

	pieces := make([]piece, len(pieceCIDs))
	for i, c := range pieceCIDs {
		pieces[i] = piece{
			PieceCID:  c,
			SubPieces: []subPiece{{SubPieceCID: c}},
		}
	}

	reqBody := struct {
		Pieces    []piece `json:"pieces"`
		ExtraData string  `json:"extraData"`
	}{
		Pieces:    pieces,
		ExtraData: extraData,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	reqURL := fmt.Sprintf("%s/pdp/data-sets/%d/pieces", CurioBaseURL(), dataSetID)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("add pieces: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("add pieces: status %d: %s", resp.StatusCode, respBody)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("add pieces: missing Location header")
	}

	parts := strings.Split(location, "/")
	txHash := parts[len(parts)-1]
	return txHash, nil
}

// WaitForPieceAddition polls until pieces are confirmed added to a dataset.
func WaitForPieceAddition(ctx context.Context, dataSetID int, txHash string) ([]int, error) {
	var pieceIDs []int
	err := poll(ctx, pollInterval, pollTimeout, func() (bool, error) {
		reqURL := fmt.Sprintf("%s/pdp/data-sets/%d/pieces/added/%s", CurioBaseURL(), dataSetID, txHash)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := curioHTTPClient().Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return false, fmt.Errorf("status %d: %s", resp.StatusCode, body)
		}

		var status PieceAdditionStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return false, err
		}
		if status.AddMessageOK != nil && *status.AddMessageOK {
			pieceIDs = status.ConfirmedPieceIDs
			return true, nil
		}
		return false, nil
	})
	return pieceIDs, err
}

// GetDataSet retrieves dataset info from Curio.
func GetDataSet(ctx context.Context, dataSetID int) (*DataSetInfo, error) {
	reqURL := fmt.Sprintf("%s/pdp/data-sets/%d", CurioBaseURL(), dataSetID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("get dataset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("dataset not found: %d", dataSetID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get dataset: status %d: %s", resp.StatusCode, body)
	}

	var info DataSetInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DownloadPiece retrieves raw piece data from Curio.
func DownloadPiece(ctx context.Context, pieceCID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", CurioBaseURL()+"/piece/"+pieceCID, nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	resp, err := curioHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download piece: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("piece not found: %s", pieceCID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download piece: status %d: %s", resp.StatusCode, body)
	}

	return io.ReadAll(resp.Body)
}

// poll retries fn at interval until it returns true, timeout, or error.
func poll(ctx context.Context, interval, timeout time.Duration, fn func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("poll timed out after %v", timeout)
		}
		done, err := fn()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
