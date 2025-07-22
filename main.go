package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// AudioQuery は /audio_query のレスポンスを表します
type AudioQuery struct {
	AccentPhrases      []interface{} `json:"accent_phrases"`
	SpeedScale         float64       `json:"speedScale"`
	PitchScale         float64       `json:"pitchScale"`
	IntonationScale    float64       `json:"intonationScale"`
	VolumeScale        float64       `json:"volumeScale"`
	PrePhonemeLength   float64       `json:"prePhonemeLength"`
	PostPhonemeLength  float64       `json:"postPhonemeLength"`
	OutputSamplingRate int           `json:"outputSamplingRate"`
	OutputStereo       bool          `json:"outputStereo"`
	Kana               string        `json:"kana"`
}

// Speaker は /speakers のレスポンスに含まれる話者情報を表します
type Speaker struct {
	Name        string         `json:"name"`
	SpeakerUUID string         `json:"speaker_uuid"`
	Styles      []SpeakerStyle `json:"styles"`
	Version     string         `json:"version"`
}

// SpeakerStyle は話者のスタイル（ノーマル、あまあま等）を表します
type SpeakerStyle struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// --- VOICEVOX APIクライアント ---

// Client はVOICEVOX APIとの通信を管理します
type Client struct {
	BaseURL string
}

// NewClient は新しいAPIクライアントを作成します
func NewClient(port int) *Client {
	return &Client{
		BaseURL: fmt.Sprintf("http://localhost:%d", port),
	}
}

// findSpeakerID は話者名から話者IDを検索します
func (c *Client) findSpeakerID(name string) (int, error) {
	resp, err := http.Get(c.BaseURL + "/speakers")
	if err != nil {
		return 0, fmt.Errorf("VOICEVOXエンジンに接続できませんでした: %v\nエンジンが起動しているか、ポート番号が正しいか確認してください", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("話者情報の取得に失敗しました (ステータスコード: %d)", resp.StatusCode)
	}

	var speakers []Speaker
	if err := json.NewDecoder(resp.Body).Decode(&speakers); err != nil {
		return 0, fmt.Errorf("話者情報のデコードに失敗しました: %v", err)
	}

	for _, speaker := range speakers {
		if speaker.Name == name {
			if len(speaker.Styles) > 0 {
				fmt.Printf("話者 '%s' (スタイル: %s, ID: %d) を使用します。\n", speaker.Name, speaker.Styles[0].Name, speaker.Styles[0].ID)
				return speaker.Styles[0].ID, nil
			}
		}
	}

	return 0, fmt.Errorf("指定された話者 '%s' が見つかりませんでした", name)
}

// listSpeakers は利用可能な話者の一覧を表示します
func (c *Client) listSpeakers() error {
	resp, err := http.Get(c.BaseURL + "/speakers")
	if err != nil {
		return fmt.Errorf("VOICEVOXエンジンに接続できませんでした: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("話者情報の取得に失敗しました (ステータスコード: %d)", resp.StatusCode)
	}

	var speakers []Speaker
	if err := json.NewDecoder(resp.Body).Decode(&speakers); err != nil {
		return fmt.Errorf("話者情報のデコードに失敗しました: %v", err)
	}

	fmt.Println("--- 利用可能な話者一覧 ---")
	for _, speaker := range speakers {
		fmt.Printf("話者名: %s\n", speaker.Name)
		for _, style := range speaker.Styles {
			fmt.Printf("  - スタイル: %s (ID: %d)\n", style.Name, style.ID)
		}
	}
	fmt.Println("--------------------------")
	fmt.Println("CLIで話者を指定する際は `--actor \"<話者名>\"` のように指定してください。")

	return nil
}

// createAudioQuery はテキストから音声合成クエリを生成します
func (c *Client) createAudioQuery(text string, speakerID int) (*AudioQuery, error) {
	endpoint := c.BaseURL + "/audio_query"
	params := url.Values{}
	params.Add("text", text)
	params.Add("speaker", strconv.Itoa(speakerID))

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("リクエストの作成に失敗しました: %v", err)
	}
	req.URL.RawQuery = params.Encode()

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio_queryリクエストに失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf(
			"audio_queryの生成に失敗しました (ステータスコード: %d)\nエラー詳細: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var query AudioQuery
	if err := json.NewDecoder(resp.Body).Decode(&query); err != nil {
		return nil, fmt.Errorf("audio_queryのデコードに失敗しました: %v", err)
	}
	return &query, nil
}

// synthesis はクエリからWAVデータを生成します
func (c *Client) synthesis(query *AudioQuery, speakerID int) ([]byte, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("クエリのJSON変換に失敗しました: %v", err)
	}

	synthesisURL := fmt.Sprintf("%s/synthesis?speaker=%d", c.BaseURL, speakerID)
	resp, err := http.Post(synthesisURL, "application/json", bytes.NewBuffer(queryJSON))
	if err != nil {
		return nil, fmt.Errorf("synthesisリクエストに失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf(
			"音声合成に失敗しました (ステータスコード: %d)\nエラー詳細: %s",
			resp.StatusCode,
			string(body),
		)
	}

	wavData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("WAVデータの読み込みに失敗しました: %v", err)
	}
	return wavData, nil
}

// --- メイン処理 ---

func main() {
	// === コマンドライン引数の定義 ===
	// 基本設定
	inputFile := flag.String("i", "", "入力テキストファイルのパス (必須)")
	outputFile := flag.String("o", "", "出力WAVファイルのパス (必須)")
	actorName := flag.String("actor", "ずんだもん", "話者の名前")
	port := flag.Int("port", 50021, "VOICEVOXエンジンのポート番号")
	showActors := flag.Bool("list-actors", false, "利用可能な話者の一覧を表示")

	// 音声パラメータ設定
	speed := flag.Float64("speed", 1.0, "話速")
	pitch := flag.Float64("pitch", 0.0, "音高（±0.15程度が推奨）")
	intonation := flag.Float64("intonation", 1.0, "抑揚")
	volume := flag.Float64("volume", 1.0, "音量")
	prePhoneme := flag.Float64("pre-phoneme", -1.0, "音声の前の無音時間 (秒)。-1でAPIのデフォルト値を使用")
	postPhoneme := flag.Float64("post-phoneme", -1.0, "音声の後の無音時間 (秒)。-1でAPIのデフォルト値を使用")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "使用法: %s [オプション]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "必須オプション:")
		fmt.Fprintln(os.Stderr, "  -i string\n    \t入力テキストファイルのパス")
		fmt.Fprintln(os.Stderr, "  -o string\n    \t出力WAVファイルのパス")
		fmt.Fprintln(os.Stderr, "\nその他のオプション:")
		flag.PrintDefaults()
	}

	flag.Parse()

	// APIクライアントを作成
	client := NewClient(*port)

	if *showActors {
		if err := client.listSpeakers(); err != nil {
			fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *inputFile == "" || *outputFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	speakerID, err := client.findSpeakerID(*actorName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("'%s' を読み込んでいます...\n", *inputFile)
	textBytes, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: ファイルの読み込みに失敗しました: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("音声合成クエリを作成中...")
	query, err := client.createAudioQuery(string(textBytes), speakerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}

	// === パラメータを上書き ===
	fmt.Println("パラメータを調整しています...")
	query.SpeedScale = *speed
	query.PitchScale = *pitch
	query.IntonationScale = *intonation
	query.VolumeScale = *volume
	if *prePhoneme != -1.0 {
		query.PrePhonemeLength = *prePhoneme
	}
	if *postPhoneme != -1.0 {
		query.PostPhonemeLength = *postPhoneme
	}


	fmt.Println("音声合成を実行中...")
	startTime := time.Now()
	wavData, err := client.synthesis(query, speakerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
	duration := time.Since(startTime)

	err = os.WriteFile(*outputFile, wavData, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: ファイルの保存に失敗しました: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✨ 完了！ (処理時間: %s)\n", duration)
	fmt.Printf("音声を '%s' に保存しました。\n", *outputFile)
}
