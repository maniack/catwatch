package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/maniack/catwatch/internal/storage"
	"github.com/sirupsen/logrus"
)

type APIClient struct {
	BaseURL       string
	PublicBaseURL string
	BotAPIKey     string
	HTTPClient    *http.Client
	log           *logrus.Logger
}

func NewAPIClient(baseURL, publicBaseURL, botAPIKey string, log *logrus.Logger) *APIClient {
	if publicBaseURL == "" {
		publicBaseURL = baseURL
	}
	return &APIClient{
		BaseURL:       baseURL,
		PublicBaseURL: publicBaseURL,
		BotAPIKey:     botAPIKey,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

func (c *APIClient) do(req *http.Request, token string) (*http.Response, error) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.BotAPIKey != "" {
		req.Header.Set("X-Bot-Key", c.BotAPIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.log.WithFields(logrus.Fields{
			"method": req.Method,
			"url":    req.URL.String(),
		}).WithError(err).Error("api request failed")
		return nil, err
	}
	if resp.StatusCode >= 400 {
		c.log.WithFields(logrus.Fields{
			"method": req.Method,
			"url":    req.URL.String(),
			"status": resp.Status,
		}).Warn("api request returned error status")
	}
	return resp, nil
}

func (c *APIClient) ListCats() ([]storage.Cat, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/cats/", c.BaseURL), nil)
	resp, err := c.do(req, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var cats []storage.Cat
	if err := json.NewDecoder(resp.Body).Decode(&cats); err != nil {
		return nil, err
	}
	return cats, nil
}

func (c *APIClient) GetCat(id string, token string) (*storage.Cat, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/cats/%s/", c.BaseURL, id), nil)
	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var cat storage.Cat
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func (c *APIClient) CreateRecord(catID string, record storage.Record, token string) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/cats/%s/records", c.BaseURL, catID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) ListRecords(catID string, status string, start, end *time.Time, token string) ([]storage.Record, error) {
	url := fmt.Sprintf("%s/api/cats/%s/records?status=%s", c.BaseURL, catID, status)
	if start != nil {
		url += "&start=" + start.Format(time.RFC3339)
	}
	if end != nil {
		url += "&end=" + end.Format(time.RFC3339)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var recs []storage.Record
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func (c *APIClient) MarkRecordDone(catID, recordID, token string) (*storage.Record, error) {
	url := ""
	if catID != "" {
		url = fmt.Sprintf("%s/api/cats/%s/records/%s/done", c.BaseURL, catID, recordID)
	} else {
		url = fmt.Sprintf("%s/api/records/%s/done", c.BaseURL, recordID)
	}
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var rec storage.Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (c *APIClient) CreateCat(cat storage.Cat, token string) (*storage.Cat, error) {
	body, err := json.Marshal(cat)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/cats/", c.BaseURL), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var out storage.Cat
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *APIClient) UpdateCat(id string, cat storage.Cat, token string) (*storage.Cat, error) {
	body, err := json.Marshal(cat)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/cats/%s/", c.BaseURL, id), bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var out storage.Cat
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *APIClient) DeleteCat(id string, token string) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/cats/%s/", c.BaseURL, id), nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) ListAllPlannedRecords(start, end time.Time) ([]storage.Record, error) {
	url := fmt.Sprintf("%s/api/records/planned?start=%s&end=%s", c.BaseURL, start.Format(time.RFC3339), end.Format(time.RFC3339))
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := c.do(req, "") // Reminder loop doesn't have a user token, but should it?
	// Actually, reminder loop might need a system token or BOT_API_KEY should be enough for some methods.
	// For now, listAllPlannedRecords might remain public or use BOT_API_KEY.
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var recs []storage.Record
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func (c *APIClient) AddCatLocation(catID string, lat, lon float64, name string, token string) error {
	in := storage.CatLocation{
		Latitude:  lat,
		Longitude: lon,
		Name:      name,
	}
	body, _ := json.Marshal(in)

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/cats/%s/locations", c.BaseURL, catID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) GetBotToken(chatID int64) (string, error) {
	in := struct {
		ChatID int64 `json:"chat_id"`
	}{ChatID: chatID}
	body, _ := json.Marshal(in)

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/bot/token", c.BaseURL), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bot token error: status %d", resp.StatusCode)
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

func (c *APIClient) UnlinkBot(chatID int64) error {
	in := struct {
		ChatID int64 `json:"chat_id"`
	}{ChatID: chatID}
	body, _ := json.Marshal(in)

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/bot/unlink", c.BaseURL), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unlink error: status %d", resp.StatusCode)
	}
	return nil
}

func (c *APIClient) DeleteUser(token string) error {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/user", c.BaseURL), nil)
	req.Header.Set("X-CSRF-Token", "bot")
	resp, err := c.do(req, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete user error: status %d", resp.StatusCode)
	}
	return nil
}

// UploadCatImage uploads image bytes via multipart/form-data to the backend.
func (c *APIClient) UploadCatImage(catID string, filename string, mime string, data []byte, token string) (*storage.Image, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/cats/%s/images", c.BaseURL, catID), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if mime != "" {
		req.Header.Set("X-File-Mime", mime)
	}

	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("upload image status: %d", resp.StatusCode)
	}
	var out storage.Image
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCatImage deletes an image by id for a cat.
func (c *APIClient) DeleteCatImage(catID, imageID, token string) (*storage.Image, error) {
	url := ""
	if catID != "" {
		url = fmt.Sprintf("%s/api/cats/%s/images/%s", c.BaseURL, catID, imageID)
	} else {
		url = fmt.Sprintf("%s/api/images/%s", c.BaseURL, imageID)
	}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("delete image status: %d", resp.StatusCode)
	}
	var out storage.Image
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *APIClient) GetCatImageBinary(catID, imageID string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/api/cats/%s/images/%s", c.BaseURL, catID, imageID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := c.do(req, "")
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *APIClient) RegisterBotUser(chatID int64, name string) error {
	in := struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}{ChatID: chatID, Name: name}
	body, _ := json.Marshal(in)

	resp, err := c.HTTPClient.Post(fmt.Sprintf("%s/api/bot/register", c.BaseURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api returned status: %d", resp.StatusCode)
	}
	return nil
}

var ErrAlreadyExists = fmt.Errorf("already exists")

func (c *APIClient) MarkNotificationSent(notification storage.BotNotification) error {
	body, _ := json.Marshal(notification)

	resp, err := c.HTTPClient.Post(fmt.Sprintf("%s/api/bot/notifications", c.BaseURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return ErrAlreadyExists
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api returned status: %d", resp.StatusCode)
	}
	return nil
}

func (c *APIClient) ListBotUsers() ([]storage.User, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/api/bot/users", c.BaseURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var users []storage.User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}
	return users, nil
}

type AuthMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	Config                struct {
		GoogleEnabled bool `json:"google_enabled"`
		OIDCEnabled   bool `json:"oidc_enabled"`
		DevLogin      bool `json:"dev_login"`
	} `json:"x_catwatch_config"`
}

func (c *APIClient) GetAuthMetadata() (*AuthMetadata, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/.well-known/oauth-authorization-server", c.BaseURL), nil)
	resp, err := c.do(req, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth metadata returned status: %d", resp.StatusCode)
	}

	var meta AuthMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
