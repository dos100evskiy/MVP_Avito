// Package coreclient — клиент Venue API core-сервиса. Инкапсулирует
// HTTP-вызовы accept/reject/status, чтобы остальной код restaurant-service
// не знал деталей контракта core (URL, заголовки, формат ошибок).
package coreclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 5 * time.Second}}
}

type Order struct {
	ID           string `json:"id"`
	VenueID      string `json:"venueId"`
	Status       string `json:"status"`
	TotalKopecks int64  `json:"totalKopecks"`
	Currency     string `json:"currency"`
	CustomerRef  string `json:"customerRef"`
}

// ListNewOrders — poll-режим получения заказов (fallback, если push-вебхук
// заведению не настроен или временно недоступен; см. CJM заведения).
func (c *Client) ListNewOrders() ([]Order, error) {
	req, err := c.newRequest(http.MethodGet, "/orders?status=CREATED", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list orders: unexpected status %d", resp.StatusCode)
	}
	var out []Order
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) AcceptOrder(orderID string) error {
	return c.postAction(fmt.Sprintf("/orders/%s/accept", orderID), nil)
}

func (c *Client) RejectOrder(orderID, reason string) error {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return c.postAction(fmt.Sprintf("/orders/%s/reject", orderID), body)
}

func (c *Client) UpdateStatus(orderID, status string) error {
	body, _ := json.Marshal(map[string]string{"status": status})
	req, err := c.newRequest(http.MethodPatch, fmt.Sprintf("/orders/%s/status", orderID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	return c.do(req)
}

func (c *Client) postAction(path string, body []byte) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := c.newRequest(http.MethodPost, path, r)
	if err != nil {
		return err
	}
	return c.do(req)
}

func (c *Client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	return req, nil
}

func (c *Client) do(req *http.Request) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("core api: unexpected status %d on %s", resp.StatusCode, req.URL.Path)
	}
	return nil
}
