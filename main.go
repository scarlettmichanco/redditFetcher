package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RedditPost structure to hold post data
type RedditPost struct {
	Title   string `json:"title"`
	Author  string `json:"author"`
	Upvotes int    `json:"score"`
}

// Stats structure to hold statistics
type Stats struct {
	TopUsers        map[string]int
	MostUpvotedPost RedditPost
}

// StatsManager manages stats safely and keeps track of rate limiting
type StatsManager struct {
	mu                sync.Mutex
	stats             Stats
	remainingRequests int
	resetTime         time.Time
}

// FetchAccessToken retrieves an OAuth2 access token from Reddit
// FetchAccessToken retrieves an OAuth2 access token from Reddit
func FetchAccessToken(client *http.Client, clientID, clientSecret string) (string, error) {
	url := "https://www.reddit.com/api/v1/access_token"
	form := "grant_type=client_credentials"

	// Create the request with the body
	req, err := http.NewRequest("POST", url, io.NopCloser(strings.NewReader(form)))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("User-Agent", "RedditFetcherCLI")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = int64(len(form)) // Set Content-Length

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get access token: %s", resp.Status)
	}

	var tokenResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", err
	}

	return tokenResponse["access_token"].(string), nil
}

	

// NewStatsManager creates a new StatsManager
func NewStatsManager() *StatsManager {
	return &StatsManager{
		stats: Stats{
			TopUsers: make(map[string]int),
		},
	}
}

// UpdateStats updates the stats based on fetched posts
func (sm *StatsManager) UpdateStats(posts []RedditPost) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, post := range posts {
		sm.stats.TopUsers[post.Author]++

		if post.Upvotes > sm.stats.MostUpvotedPost.Upvotes {
			sm.stats.MostUpvotedPost = post
		}
	}
}

// PrintStats prints the current statistics and rate limit info
func (sm *StatsManager) PrintStats() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Print most upvoted post
	if sm.stats.MostUpvotedPost.Title != "" {
		fmt.Println("Most Upvoted Post:", sm.stats.MostUpvotedPost.Title)
		fmt.Println("Upvotes:", sm.stats.MostUpvotedPost.Upvotes)
	}

	// Print top users by post count
	fmt.Println("Top Users by Post Count:")
	for user, count := range sm.stats.TopUsers {
		fmt.Printf("%s: %d posts\n", user, count)
	}

	// Print rate limit info if available
	if sm.remainingRequests > 0 {
		fmt.Printf("Remaining requests: %d\n", sm.remainingRequests)
		if !sm.resetTime.IsZero() {
			countdown := time.Until(sm.resetTime).Seconds()
			fmt.Printf("Rate limit will reset in: %.0f seconds\n", countdown)
		}
	} else {
		fmt.Println("Rate limit exceeded. Please wait for reset.")
		if !sm.resetTime.IsZero() {
			countdown := time.Until(sm.resetTime).Seconds()
			fmt.Printf("You can resume requests in: %.0f seconds\n", countdown)
		}
	}
	fmt.Println() // Print an empty line for better readability
}

func FetchRedditData(client *http.Client, subreddit string, sm *StatsManager, token string, wg *sync.WaitGroup) {
	defer wg.Done()

	url := "https://www.reddit.com/r/" + subreddit + "/new.json?limit=10"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token) // Add token here
	req.Header.Set("User-Agent", "RedditFetcherCLI")

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error fetching data:", err)
		return
	}
	defer resp.Body.Close()

	// Update remaining requests and reset time based on headers
	if resp.StatusCode == 429 {
		log.Println("Rate limit exceeded. Waiting...")
		return
	}

	// Check headers for rate limit information
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		fmt.Sscanf(remaining, "%d", &sm.remainingRequests)
		fmt.Println("Remaining Requests:", sm.remainingRequests)
	}

	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		resetUnix, _ := strconv.ParseInt(reset, 10, 64)
		sm.resetTime = time.Unix(resetUnix, 0)
		fmt.Println("Rate Limit Reset Time:", sm.resetTime)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Println("Error decoding response:", err)
		return
	}

	var posts []RedditPost
	for _, child := range result["data"].(map[string]interface{})["children"].([]interface{}) {
		postData := child.(map[string]interface{})["data"].(map[string]interface{})
		posts = append(posts, RedditPost{
			Title:  postData["title"].(string),
			Author: postData["author"].(string),
			Upvotes: int(postData["score"].(float64)),
		})
	}

	log.Println("Fetching data from subreddit:", subreddit)
	fmt.Println("Remaining requests:", sm.remainingRequests)

	sm.UpdateStats(posts) // Update stats with the fetched posts
}

// Stats printing loop
func StartStatsPrinting(sm *StatsManager, interval time.Duration) {
	for {
		time.Sleep(interval)
		sm.PrintStats() // Print stats at regular intervals
	}
}

// Main function
func main() {
	client := &http.Client{}

	clientID := "ey9AWAIGj6rn18bQIecGRw"     // Replace with your client ID
	clientSecret := "HcUzeI3XIHCYGeDjx3DIE_mwX9doGA" // Replace with your client secret

	// Fetch access token
	token, err := FetchAccessToken(client, clientID, clientSecret)
	if err != nil {
		log.Fatal("Error fetching access token:", err)
		return
	}

	// Create a StatsManager instance
	sm := NewStatsManager()

	// Set the subreddit to fetch data from
	subreddit := "golang" // Replace with your chosen subreddit

	// Start a Goroutine to print stats every 10 seconds
	go StartStatsPrinting(sm, 10*time.Second)

	// Rate limit interval (e.g., 2 seconds)
	limitInterval := 5 * time.Second

	// Set up a loop to continuously fetch data
	for {
		// Rate limiting: Check remaining requests before fetching
		if sm.remainingRequests > 0 {
			wg := sync.WaitGroup{}
			wg.Add(1)
			go FetchRedditData(client, subreddit, sm, token, &wg) // Pass token here
			wg.Wait() // Wait for the fetching to complete
		} else {
			// Wait for the rate limit to reset
			countdown := time.Until(sm.resetTime).Seconds()
			if countdown > 0 {
				fmt.Printf("Waiting for %.0f seconds before making a new request...\n", countdown)
				time.Sleep(time.Duration(countdown) * time.Second)
			}
		}

		// Wait before the next request
		time.Sleep(limitInterval) // Adjust based on your needs
	}
}
