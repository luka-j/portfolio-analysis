package sandbox

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"portfolio-analysis/models"
)

// Service provides isolated execution of Python code.
type Service struct {
	pythonPath   string
	runnerPath   string
	enabled      bool
	timeout      time.Duration
}

// Result is the output of a sandbox execution.
type Result struct {
	Output string
	Error  string
}

type payload struct {
	Code         string `json:"code"`
	DataPath     string `json:"data_path"`
	CashDataPath string `json:"cash_data_path"`
}

// NewService creates a new sandbox service. It verifies that Python is available
// and that the required libraries (pandas, numpy) are installed.
func NewService() *Service {
	svc := &Service{
		timeout: 30 * time.Second,
	}

	// Check if Python sandbox is enabled via env var
	if os.Getenv("PYTHON_SANDBOX_ENABLED") != "true" {
		slog.Info("sandbox: Python sandbox disabled (PYTHON_SANDBOX_ENABLED != true)")
		return svc
	}

	// Try to find python3 or python
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		pythonPath, err = exec.LookPath("python")
	}

	if err != nil {
		slog.Warn("sandbox: Python executable not found, sandbox will be disabled")
		return svc
	}

	// Check if pandas and numpy are installed
	cmd := exec.Command(pythonPath, "-c", "import pandas, numpy")
	if err := cmd.Run(); err != nil {
		slog.Warn("sandbox: Python pandas/numpy not installed, sandbox will be disabled")
		return svc
	}

	// Verify runner script exists
	runnerPath := filepath.Join(".", "sandbox_runner.py")
	if _, err := os.Stat(runnerPath); err != nil {
		slog.Warn("sandbox: sandbox_runner.py not found, sandbox will be disabled", "path", runnerPath)
		return svc
	}

	svc.pythonPath = pythonPath
	svc.runnerPath = runnerPath
	svc.enabled = true
	slog.Info("sandbox: Python sandbox initialized successfully", "path", pythonPath)
	return svc
}

// IsEnabled returns true if the sandbox is available and properly configured.
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// Execute runs the provided Python code in the sandbox.
func (s *Service) Execute(ctx context.Context, code string, trades []models.Trade, cashTxns []models.CashTransaction) (*Result, error) {
	if !s.enabled {
		return nil, fmt.Errorf("python sandbox is not enabled")
	}

	// Create temp directory for CSVs
	tmpDir, err := os.MkdirTemp("", "sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tradesPath := filepath.Join(tmpDir, "trades.csv")
	cashPath := filepath.Join(tmpDir, "cash.csv")

	if err := writeTradesCSV(tradesPath, trades); err != nil {
		return nil, fmt.Errorf("writing trades csv: %w", err)
	}
	if err := writeCashCSV(cashPath, cashTxns); err != nil {
		return nil, fmt.Errorf("writing cash csv: %w", err)
	}

	p := payload{
		Code:         code,
		DataPath:     tradesPath,
		CashDataPath: cashPath,
	}

	pBytes, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, s.pythonPath, s.runnerPath)
	cmd.Stdin = bytes.NewReader(pBytes)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Optional: add process restrictions here if on Linux using syscall.SysProcAttr
	// (Omitted here for cross-platform compatibility, relying on Docker's isolation)

	err = cmd.Run()
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return &Result{Error: "Execution timed out after " + s.timeout.String()}, nil
		}
		
		// If the process failed entirely (e.g. syntax error that broke the runner before JSON output)
		if stdout.Len() == 0 {
			return nil, fmt.Errorf("python execution failed: %v, stderr: %s", err, stderr.String())
		}
	}

	var res Result
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return nil, fmt.Errorf("parsing sandbox output: %w (stdout: %s)", err, stdout.String())
	}

	// Append stderr to the error if present to help the LLM debug
	if stderr.Len() > 0 {
		if res.Error != "" {
			res.Error += "\n\nStandard Error:\n" + stderr.String()
		} else {
			res.Error = "Standard Error:\n" + stderr.String()
		}
	}

	return &res, nil
}

func writeTradesCSV(path string, trades []models.Trade) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header
	header := []string{
		"symbol", "date_time", "quantity", "price", "proceeds",
		"commission", "buy_sell", "currency", "asset_category",
		"listing_exchange", "isin",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, t := range trades {
		record := []string{
			t.Symbol,
			t.DateTime.Format(time.RFC3339),
			strconv.FormatFloat(t.Quantity, 'f', -1, 64),
			strconv.FormatFloat(t.Price, 'f', -1, 64),
			strconv.FormatFloat(t.Proceeds, 'f', -1, 64),
			strconv.FormatFloat(t.Commission, 'f', -1, 64),
			t.BuySell,
			t.Currency,
			t.AssetCategory,
			t.ListingExchange,
			t.ISIN,
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return nil
}

func writeCashCSV(path string, cash []models.CashTransaction) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header
	header := []string{"type", "symbol", "currency", "amount", "date_time", "description"}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, c := range cash {
		record := []string{
			c.Type,
			c.Symbol,
			c.Currency,
			strconv.FormatFloat(c.Amount, 'f', -1, 64),
			c.DateTime.Format(time.RFC3339),
			c.Description,
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return nil
}
