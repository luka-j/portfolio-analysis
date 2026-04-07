package llm

import "strings"

// CannedPrompt holds the configuration for a predefined prompt.
type CannedPrompt struct {
	Message           string // prompt text; may contain {key} placeholders filled via Render
	SystemInstruction string // if non-empty, overrides the default system instruction
	ChatAccessible    bool   // if true, available as a prompt_type on POST /llm/chat
	Cacheable         bool   // if true, responses are cached 24 h (ChatAccessible prompts only)
}

// Render replaces {key} placeholders in Message with the provided vars.
func (cp CannedPrompt) Render(vars map[string]string) string {
	if len(vars) == 0 {
		return cp.Message
	}
	args := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		args = append(args, "{"+k+"}", v)
	}
	return strings.NewReplacer(args...).Replace(cp.Message)
}

const defaultConstraints = `<constraints>
- DO NOT provide overly specific personalized financial advice (e.g., never say "You should sell X").
- DO NOT invent or hallucinate news events. If you are unsure about recent news for a ticker, state that explicitly.
- DO NOT speculate on exact future price targets, only on ranges and only when backed up with a multitude of sources, carefully citing them.
</constraints>`

// CannedPrompts is the registry of all predefined prompts.
var CannedPrompts = map[string]CannedPrompt{
	// market_summary is used exclusively by GET /llm/summary; not accessible via chat.
	"market_summary": {
		Message: `Provide a brief market summary formatted as two short bullet points covering {period}:
- **Macro:** Focus on macroeconomic factors that had the biggest impact on the broader market (mention major global indices if relevant).
- **Portfolio:** Very briefly explain the biggest market movements in the <portfolio_data> provided, citing specific tickers, sectors, or significant currency (FX) impacts if applicable.

Keep each bullet point under 30 words.`,
		SystemInstruction: "You are an expert, professional financial analyst. Provide concise, impactful summaries. Use short information-filled sentences. Avoid overly long compound sentences. Respect the length requirement.",
		ChatAccessible:    false,
	},
	"general_analysis": {
		Message: `Analyze my current portfolio given current market conditions. What am I effectively betting on?

First, use a <thinking> block to identify the major holdings, their primary sector exposure, and the current macroeconomic narratives surrounding them.

Then, structure your response using exactly these markdown headers:
### 🌍 Macro Environment
Briefly summarize the overarching market regime (e.g., inflationary, rate-cutting cycle, tech-led growth) and how this portfolio aligns with it.
### 📊 Sector & Geographic Concentration
Identify where my money is truly concentrated (do not just list percentages; Group them by common themes like "AI infrastructure" or regional exposures like "US Tech versus European Industrials").
### 🎯 Implicit Bets
Based on my concentration, what specific future events, market shifts, or currency dynamics am I effectively betting heavily on to happen? What am I most vulnerable to (e.g. exposed to a weakening USD)?
### 🔍 Blind Spots & Under-allocations
What obvious market sectors, geographical regions, defensive assets, or FX hedges am I completely un-hedged against or missing out on entirely?`,
		ChatAccessible: true,
		Cacheable:      true,
	},
	"best_worst_scenarios": {
		Message: `Analyze my current portfolio's exposure to market volatility.

First, use a <thinking> block to map out specific, realistic macroeconomic and industry-specific catalysts that could drastically affect my major holdings.

Then, outside of the block, explain the scenarios using these headers:
### 🌟 Best Case Scenario
Describe a highly favorable, yet realistic sequence of events over the next 6-12 months that would cause this portfolio to significantly outperform. Exactly which catalysts would need to align?
### ⛈️ Worst Case Scenario
Describe a realistic stress scenario (e.g., specific regulatory shifts, supply chain shocks, currency headwinds, or rate changes) that would cause this portfolio to suffer heavy drawdowns. What is the structural weakness?
### 📡 Key Indicators to Watch
List 2-3 specific, measurable macroeconomic or fundamental data points I should monitor closely to see which of the two scenarios is actively unfolding (e.g., upcoming inflation data, central bank meetings like the Fed or ECB, or key sector earnings).`,
		ChatAccessible: true,
		Cacheable:      true,
	},
	"ticker_analysis": {
		Message: `You are analyzing the asset: {label}.

First, use the Google Search tool to find the most recent financial news, earnings reports, and current sentiment analysis for this specific ticker. Use a <thinking> block to synthesize your findings and verify you have up-to-date context.

Then, analyze the asset through four specific lenses:
1. **Catalysts:** (Recent earnings, regulatory filings, product launches, or macro shifts impacts).
2. **Sentiment:** (Shift in institutional vs. retail buzz, analyst upgrades/downgrades).
3. **Peer Comparison & Valuation:** (How is it performing relative to its closest competitors or sector averages? Note any obvious valuation metrics if found in recent news).
4. **Technical Context:** (Where does the price currently hover relative to recent historical highs/lows or moving averages?).

Provide a bolded "**Bottom Line**" summary followed by a bulleted breakdown of clear **Risks** and **Opportunities**.`,
		SystemInstruction: "You are an expert equity research analyst. Always ground your analysis in recent, real-world data across the broader market. Actively use search tools.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
	"risk_metrics": {
		Message: `Please interpret the following portfolio data in plain English, focusing on "The Story of the Money" rather than just the math.

<portfolio_stats>
{data_json}
</portfolio_stats>

First, use a <thinking> block to break down each metric and what it implies about the investor's behavior and risk tolerance.

Then, provide your analysis structured around these points:
1. **The Returns Narrative**: Explain the difference between my TWR and MWR. Am I a good "timer" of my own deposits/withdrawals, or is my behavior costing me money? 
2. **Wealth Growth**: Based on the VAMI, how has the "purchasing power" of this portfolio changed? 
3. **The Efficiency Test**: Using Sharpe and Sortino, tell me if I'm taking "productive" risk or "reckless" risk. Is the "downside pain" (Sortino) significantly different from the "total swing" (Sharpe)? 
4. **The Stress Test**: Contextualize the Max Drawdown. Is this level of loss sustainable for a long-term investor? 
5. **Investor Profile**: Is this portfolio more suited for the aggressive growth investor, defensive value-preservation investor, or neither? 
6. **The Verdict**: Is this a "smooth ride" or a "rollercoaster," and am I being rewarded for staying on it?`,
		SystemInstruction: "Act as a private wealth manager performing a year-end review for a client. Speak directly to the client.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
	"benchmark_analysis": {
		Message: `I am comparing my portfolio against {benchmark}.

<portfolio_benchmark>
{data_json}
</portfolio_benchmark>

First, use a <thinking> block to compare my portfolio's metrics against the benchmark's assumed baseline, evaluating the Alpha, Beta, and Tracking Error. Consider what combinations of these metrics imply (e.g., high Tracking Error + negative Alpha = poor active management).

Then, provide a "so what?" analysis of my performance based on the provided data. 

**ANALYSIS REQUIREMENTS**: 
1. **Manager Skill vs. Luck**: Based on the Alpha and Information Ratio, is the outperformance (if any) consistent or just a result of high active risk? Is the Alpha statistically meaningful?
2. **Risk Profile**: Use Beta and Treynor to explain if I am being properly compensated for the systematic risk I'm taking. Am I simply leveraging up market risk?
3. **Benchmarking**: Use Correlation and Tracking Error to tell me if this portfolio is a "closet indexer" or if it truly deviates from the benchmark in a meaningful way. 
4. **Investor Profile**: Is this portfolio better suited for an aggressive growth investor or a defensive value-preservation investor expecting downside protection? 
5. **Verdict**: Give me a blunt, executive summary of whether this portfolio is efficiently managed relative to the benchmark.`,
		SystemInstruction: "Act as an institutional portfolio analyst reviewing a fund manager's performance against a benchmark index.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
	"upcoming_events": {
		Message: `Analyze the upcoming events that may impact my current portfolio over the next 30 days. Today's date is {current_date}.

First, use the Google Search tool to find scheduled earnings reports, upcoming macroeconomic data releases (e.g., inflation prints, central bank meetings like the Fed/ECB), and pertinent geopolitical events scheduled to occur within the next month that directly affect my major holdings. Use a <thinking> block to synthesize your findings, drop any events outside the 30-day window, and verify you have exact dates where possible.

Then, categorize and summarize the upcoming events that may affect my portfolio. Structure your response using exactly these markdown headers:
### 📅 Scheduled Earnings & Corporate Events
List key upcoming earnings or shareholder meetings within the next 30 days for my holdings. Include specific dates. Explain briefly why each is critical.
### 🏦 Macroeconomic & Central Bank Events
Identify exact upcoming economic data releases or rate decisions occurring this month that could impact my asset allocation.
### 🌍 Market & Geopolitical Catalysts
Highlight broader global events or ongoing developments reaching critical milestones this month that could influence my portfolio composition.
### ⚠️ Key Portfolio Risks & Opportunities
Summarize the most significant short-term risks or opportunities based specifically on this 30-day calendar of events.`,
		SystemInstruction: "You are an expert financial analyst. Focus strictly on near-term calendar events (next 30 days). Discard any hypothetical scenarios or long-term trends. Always use exact dates when available.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
}

// IsValidCannedType returns true if the specified type is a chat-accessible registered prompt.
func IsValidCannedType(promptType string) bool {
	cp, ok := CannedPrompts[promptType]
	return ok && cp.ChatAccessible
}
