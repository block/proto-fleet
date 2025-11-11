package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	serverURL  = "http://localhost:7071"
	jsonOutput bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "minefield-cli",
		Short: "CLI for Minefield error injection proxy",
		Long:  "Control the Minefield proxy to inject errors into miner API responses",
	}

	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "http://localhost:7071", "Control API server URL")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Add commands
	rootCmd.AddCommand(
		triggerCmd(),
		listCmd(),
		clearCmd(),
		statusCmd(),
		definitionsCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// triggerCmd creates the trigger command
func triggerCmd() *cobra.Command {
	var (
		errorLevel string
		message    string
		params     []string
		ttl        int
	)

	cmd := &cobra.Command{
		Use:   "trigger <error-code>",
		Short: "Trigger a new error",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			errorCode := args[0]

			// Parse parameters
			details := make(map[string]interface{})
			for _, p := range params {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) != 2 {
					fmt.Fprintf(os.Stderr, "Invalid parameter format: %s (expected key=value)\n", p)
					os.Exit(1)
				}

				key := parts[0]
				value := parts[1]

				// Try to parse as number first
				if num, err := strconv.ParseFloat(value, 64); err == nil {
					details[key] = num
				} else if value == "true" || value == "false" {
					details[key] = value == "true"
				} else {
					details[key] = value
				}
			}

			// Build request
			reqBody := map[string]interface{}{
				"error_code": errorCode,
				"details":    details,
			}

			if errorLevel != "" {
				reqBody["error_level"] = errorLevel
			}
			if message != "" {
				reqBody["message"] = message
			}
			if ttl > 0 {
				reqBody["ttl_seconds"] = ttl
			}

			// Send request
			resp, err := postJSON(serverURL+"/api/errors/trigger", reqBody)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error triggering: %v\n", err)
				os.Exit(1)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(resp, &result); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Println(string(resp))
			} else {
				fmt.Printf("Error triggered successfully!\n")
				fmt.Printf("ID: %s\n", result["id"])
				fmt.Printf("Code: %s\n", result["error_code"])
				fmt.Printf("Level: %s\n", result["error_level"])
			}
		},
	}

	cmd.Flags().StringVarP(&errorLevel, "level", "l", "", "Error level (Error|Warning)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Error message")
	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Error parameters (key=value)")
	cmd.Flags().IntVarP(&ttl, "ttl", "t", 0, "TTL in seconds")

	return cmd
}

// listCmd creates the list command
func listCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List errors",
		Run: func(cmd *cobra.Command, args []string) {
			endpoint := "/api/errors/active"
			if all {
				endpoint = "/api/errors/all"
			}

			resp, err := getRequest(serverURL + endpoint)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching errors: %v\n", err)
				os.Exit(1)
			}

			var errors []map[string]interface{}
			if err := json.Unmarshal(resp, &errors); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Println(string(resp))
			} else {
				if len(errors) == 0 {
					fmt.Println("No errors found")
					return
				}

				fmt.Printf("%-36s %-20s %-10s %-20s %s\n", "ID", "CODE", "LEVEL", "TIME", "MESSAGE")
				fmt.Println(strings.Repeat("-", 100))

				for _, err := range errors {
					id := err["id"].(string)
					code := err["error_code"].(string)
					level := err["error_level"].(string)
					message := err["message"].(string)

					// Format time
					insertedAt := int64(err["inserted_at"].(float64))
					timeStr := time.Unix(insertedAt, 0).Format("2006-01-02 15:04:05")

					// Check if expired
					if expired, ok := err["expired_at"].(float64); ok && expired > 0 {
						timeStr += " (expired)"
					}

					fmt.Printf("%-36s %-20s %-10s %-20s %s\n", id, code, level, timeStr, message)
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all errors including expired")

	return cmd
}

// clearCmd creates the clear command
func clearCmd() *cobra.Command {
	var clearAll bool

	cmd := &cobra.Command{
		Use:   "clear [error-id]",
		Short: "Clear errors",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if clearAll {
				if err := deleteRequest(serverURL + "/api/errors"); err != nil {
					fmt.Fprintf(os.Stderr, "Error clearing all errors: %v\n", err)
					os.Exit(1)
				}
				fmt.Println("All errors cleared")
			} else if len(args) == 1 {
				if err := deleteRequest(serverURL + "/api/errors/" + args[0]); err != nil {
					fmt.Fprintf(os.Stderr, "Error clearing error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Error %s cleared\n", args[0])
			} else {
				fmt.Fprintln(os.Stderr, "Please provide an error ID or use --all flag")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVarP(&clearAll, "all", "a", false, "Clear all errors")

	return cmd
}

// statusCmd creates the status command
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Get proxy status",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := getRequest(serverURL + "/api/status")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching status: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Println(string(resp))
			} else {
				var status map[string]interface{}
				if err := json.Unmarshal(resp, &status); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
					os.Exit(1)
				}

				fmt.Printf("Status: %s\n", status["status"])
				fmt.Printf("Active errors: %v\n", status["active_errors"])
				fmt.Printf("Total errors: %v\n", status["total_errors"])
			}
		},
	}
}

// definitionsCmd creates the definitions command
func definitionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "definitions",
		Short: "List available error definitions",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := getRequest(serverURL + "/api/errors/definitions")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching definitions: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Println(string(resp))
			} else {
				var definitions []map[string]interface{}
				if err := json.Unmarshal(resp, &definitions); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
					os.Exit(1)
				}

				fmt.Printf("%-30s %-15s %-10s %s\n", "CODE", "CATEGORY", "LEVEL", "DESCRIPTION")
				fmt.Println(strings.Repeat("-", 90))

				for _, def := range definitions {
					code := def["code"].(string)
					category := def["category"].(string)
					level := def["default_level"].(string)
					desc := def["description"].(string)

					fmt.Printf("%-30s %-15s %-10s %s\n", code, category, level, desc)

					// Show parameters
					if params, ok := def["parameters"].([]interface{}); ok && len(params) > 0 {
						fmt.Print("  Parameters: ")
						paramStrs := make([]string, 0)
						for _, p := range params {
							param := p.(map[string]interface{})
							name := param["name"].(string)
							if required, ok := param["required"].(bool); ok && required {
								name += "*"
							}
							paramStrs = append(paramStrs, name)
						}
						fmt.Println(strings.Join(paramStrs, ", "))
					}
				}

				fmt.Println("\n* = required parameter")
			}
		},
	}
}

// HTTP helper functions

func getRequest(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func postJSON(url string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func deleteRequest(url string) error {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}