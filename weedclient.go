package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
)

type WeedClient struct {
	master string
}

type assignResp struct {
	Count     int    `json:"count"`
	Fid       string `json:"fid"`
	Url       string `json:"url"`
	PublicUrl string `json:"publicUrl"`
}

func (c *WeedClient) Upload(ctx context.Context, buf []byte) (string, error) {
	resp, err := httpGetC(ctx, c.master+"/dir/assign")
	if err != nil {
		return "", err
	}
	var ar assignResp
	err = json.NewDecoder(resp.Body).Decode(&ar)
	resp.Body.Close()
	if err != nil {
		return "", err
	}

	body := bytes.NewBuffer(make([]byte, 0, len(buf)+4096))
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "chunk.dat")
	if err != nil {
		return "", err
	}
	if _, err = part.Write(buf); err != nil {
		return "", err
	}
	writer.Close()
	req, err := http.NewRequest(http.MethodPost, weedURL(ar.Url, ar.Fid), body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err = http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}

	resp.Body.Close()

	return ar.Fid, nil
}

type lookupData struct {
	Locations []struct {
		PublicUrl string `json:"publicUrl"`
		Url       string `json:"url"`
	} `json:"locations"`
}

func weedURL(volume, file string) string {
	return "http://" + volume + "/" + file
}

func httpGetC(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req.WithContext(ctx))
}

func (c *WeedClient) lookupVolume(ctx context.Context, volumeID string) (string, error) {
	resp, err := httpGetC(ctx, c.master+"/dir/lookup?volumeId="+volumeID)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var ld lookupData
	if err = json.NewDecoder(resp.Body).Decode(&ld); err != nil {
		return "", err
	}
	return ld.Locations[0].Url, nil
}

func (c *WeedClient) Get(ctx context.Context, fileID string) (*http.Response, error) {
	vol, err := c.lookupVolume(ctx, fileID)
	if err != nil {
		return nil, err
	}
	return httpGetC(ctx, weedURL(vol, fileID))
}

func (c *WeedClient) Delete(ctx context.Context, fileID string) error {
	vol, err := c.lookupVolume(ctx, fileID)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, weedURL(vol, fileID), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
