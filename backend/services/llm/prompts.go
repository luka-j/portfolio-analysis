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
- DO NOT provide overly specific personalized financial advice (e.g., never say "You should sell X", unless explicitly prompted).
- DO NOT invent or hallucinate news events. If you are unsure about recent news for a ticker, state that explicitly.
- DO NOT speculate on exact future price targets, only on ranges and only when backed up with a multitude of sources, carefully citing them.
- TICKER SYMBOLS ARE AUTHORITATIVE: every symbol and name in the portfolio data is exact and correct. Never silently correct, substitute, or confuse a ticker with a more commonly known one (e.g. "SPP1" is the Vanguard FTSE All-World EUR-hedged ETF — it is NOT a misspelling of the S&P 500 or any S&P 500 instrument). If a ticker is unfamiliar, look it up rather than assuming it refers to something more popular. If search results conflict with the name provided in the portfolio data, the portfolio data is the ground truth — do not let search results override or reinterpret the provided ticker-to-name mapping.
- OMIT RATHER THAN FABRICATE: if you have no meaningful, well-grounded content for a section (e.g. no relevant recent news, no clear factor tilt, no identifiable risk), write "Nothing significant to report." for that section instead of filling it with vague or speculative filler. No information is better than low-quality information.
</constraints>`

// CannedPrompts is the registry of all predefined prompts.
var CannedPrompts = map[string]CannedPrompt{
	// market_summary is used exclusively by GET /llm/summary; not accessible via chat.
	// long_market_summary is the chat-accessible expanded version triggered from the landing page.
	"long_market_summary": {
		Message: `Provide a comprehensive market briefing covering {period}.

Use the Google Search tool to find relevant news, economic data releases, and company-specific developments. Use a <thinking> block to gather and synthesize your findings before writing the final briefing.

Structure your response using exactly these markdown headers:
### 🌍 Macro Environment
Summarize the major macroeconomic developments over {period}: monetary policy signals, inflation or employment data, central bank decisions (Fed, ECB, BoJ, etc.), and how major global indices (S&P 500, STOXX 600, Nikkei, etc.) responded. Focus on the why behind the moves, not just the what. If the VIX moved meaningfully over the period, note its direction and what it signals about risk appetite.
### 📊 Portfolio Impact
Explain how the macro backdrop specifically affected the portfolio's major holdings and sectors over {period}. Cite specific tickers, sector-level moves, and any notable FX dynamics that influenced returns.
### 🔬 Company-Specific Spotlight
Highlight major news events directly involving portfolio constituents over {period}: significant earnings releases and guidance updates, analyst upgrades or downgrades with material changes in price targets, and notable insider buying or selling. Only include developments with a meaningful price or sentiment impact — omit routine noise.
### 🌊 Sentiment & Flows
Describe the prevailing market sentiment over {period}: direction of the Fear & Greed index or comparable sentiment gauges, notable put/call ratio or options skew shifts, and any significant fund flow data (sectors or geographies seeing meaningful inflows or outflows). Qualify any figures as approximate and note if data is unavailable rather than estimating.
### 💱 Currency & Commodity Pulse
Summarize notable moves in major FX pairs (EUR/USD, USD/JPY, and any pairs directly relevant to the portfolio's geographic exposure) and key commodities (oil, gold, copper as a growth proxy) over {period}. Where relevant, connect these moves to the portfolio's hedged or unhedged positions.`,
		SystemInstruction: "You are an expert, professional financial analyst writing a comprehensive market briefing. Ground every claim in real, verifiable data from your search results. Prioritize depth and accuracy over brevity.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
	"market_summary": {
		Message: `Provide a brief market summary formatted as two short bullet points covering {period}:
- **Macro:** Focus on macroeconomic factors that had the biggest impact on the broader market (mention major global indices if relevant).
- **Portfolio:** Very briefly explain the biggest market movements in the <portfolio_data> provided, citing specific tickers, sectors, or significant currency (FX) impacts if applicable.

Keep each bullet point under 30 words.`,
		SystemInstruction: "You are an expert, professional financial analyst. Provide concise, impactful summaries. Use short information-filled sentences. Avoid overly long compound sentences. Respect the length requirement.",
		ChatAccessible:    false,
	},
	"add_or_trim": {
		Message: `You are helping me decide where to allocate new money and where to reduce existing positions within my current portfolio. Do NOT suggest adding any new securities — only work with what I already hold.

Use the Google Search tool to find current news, recent earnings, analyst sentiment, and valuation context for my holdings. Use a <thinking> block, and begin it by listing every ticker from the portfolio data alongside its full name exactly as provided — do not paraphrase or infer names. Use this list as your reference throughout; if a search result describes a security whose name does not match the provided name for that ticker, discard it and search more specifically. Then evaluate each position's upside and downside case, and identify any that are already overrepresented relative to their risk/reward.

Then structure your response using exactly these markdown headers:
### ✅ Add Weight To
Pick exactly three holdings from my portfolio with the most compelling case for putting more money in right now. For each, write 2–4 sentences covering: the core upside catalyst, why it complements the rest of the portfolio (diversification, factor tilt, or thematic fit), and any key risk to monitor. Lead each entry with the ticker in bold.
### ✂️ Trim or Avoid
Pick exactly three holdings from my portfolio where the case for adding more money is weakest — either due to a credible downside story, stretched valuation, or because the position is already overrepresented relative to the rest of the portfolio. For each, write 2–4 sentences covering: the core concern, what conditions would make this thesis wrong (i.e. when you'd change your mind), and whether this is a full exit candidate or just a "don't add" signal. Lead each entry with the ticker in bold.

A holding may appear in both lists if it represents a high-conviction asymmetric bet — compelling upside but with an equally credible downside that warrants caution before sizing up.`,
		SystemInstruction: "You are an expert equity analyst helping a client make capital allocation decisions within their existing portfolio. Ground your recommendations in current, real-world data from your search results. Be direct and opinionated — avoid hedging every sentence.",
		ChatAccessible:    true,
		Cacheable:         false,
	},
	"general_analysis": {
		Message: `Analyze my current portfolio given current market conditions. What am I effectively betting on?

First, use a <thinking> block to identify the major holdings, their primary sector exposure, and the current macroeconomic narratives surrounding them.

Then, structure your response using exactly these markdown headers:
### 🌍 Macro Environment
Briefly summarize the overarching market regime (e.g., inflationary, rate-cutting cycle, tech-led growth) and how this portfolio aligns with it.
### 📊 Sector & Geographic Concentration
Identify where my money is truly concentrated (do not just list percentages; Group them by common themes like "AI infrastructure" or regional exposures like "US Tech versus European Industrials").
### 🧮 Fama-French Factor Tilts
Provide a qualitative, one-paragraph assessment of this portfolio's likely factor tilts using the Fama-French five-factor framework (Market, Size, Value/Growth, Profitability, Investment). 
Do not attempt precise calculations — instead reason from the holdings' known characteristics (e.g. mega-cap growth tech = strong negative HML, strong positive RMW; broad market ETFs = near-zero SMB). 
Conclude with one sentence on whether the combined tilt profile is deliberate or incidental.
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
List 2-3 specific, measurable macroeconomic or fundamental data points I should monitor closely to see which of the two scenarios is actively unfolding (e.g., upcoming inflation data, central bank meetings like the Fed or ECB, or key sector earnings).
### 📜 Historical Precedents
Identify 2-3 specific historical periods (e.g., the 2000 dot-com bust, 2008 GFC, 2020 COVID crash, 2022 rate-hike cycle) where a portfolio with a similar geographic, sector, and asset-type composition faced comparable market conditions. For each period, briefly describe how such a portfolio would likely have performed — both during the drawdown and the subsequent recovery — and what the key driver of that outcome was.`,
		ChatAccessible: true,
		Cacheable:      true,
	},
	"ticker_analysis": {
		Message: `You are analyzing the asset: {label}.

First, use a <thinking> block and open it by stating the ticker symbol and its full name exactly as provided above — this is your ground truth. Then use the Google Search tool to find the most recent financial news, earnings reports, and current sentiment analysis, searching specifically for that ticker symbol and name. If a search result describes a different security, discard it and refine your search. Synthesize only findings that unambiguously match the provided ticker and name.

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
