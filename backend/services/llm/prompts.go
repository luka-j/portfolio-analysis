package llm

import (
	"strings"

	"google.golang.org/genai"
)

// CannedPrompt holds the configuration for a predefined prompt.
type CannedPrompt struct {
	Message           string // prompt text; may contain {key} placeholders filled via Render
	SystemInstruction string // if non-empty, overrides the default system instruction
	ChatAccessible    bool   // if true, available as a prompt_type on POST /llm/chat
	Cacheable         bool   // if true, responses are cached 24 h (ChatAccessible prompts only)

	// ForcedTool, when non-empty, sets ToolChoice to force the model to call this tool first.
	// This guarantees the agent fetches the required data before generating its analysis.
	ForcedTool string

	// Schema, when non-nil, enables structured JSON output via ResponseSchema.
	// Only applicable to prompts that do NOT use ForcedTool (tool-first prompts stream freely).
	Schema *genai.Schema

	// SectionOrder defines the ordered field keys for markdown reconstruction and frontend rendering.
	// "thinking" is always handled separately (collapsible disclosure) and must be listed first.
	SectionOrder []string

	// SectionTitles maps each non-thinking field key to its display title.
	SectionTitles map[string]string
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
- SIMULATED SCENARIOS: The simulate_scenario tool builds a hypothetical portfolio for analysis. Its results are counterfactual; never present them as the user's real holdings. Always state 'in this hypothetical' when discussing its output.
</constraints>`

// stringSchema is a convenience helper for a simple string field schema.
func stringSchema() *genai.Schema { return &genai.Schema{Type: genai.TypeString} }

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
- **Portfolio:** Very briefly explain the biggest market movements in the portfolio data provided, citing specific tickers, sectors, or significant currency (FX) impacts if applicable.

Keep each bullet point under 30 words.`,
		SystemInstruction: "You are an expert, professional financial analyst. Provide concise, impactful summaries. Use short information-filled sentences. Avoid overly long compound sentences. Respect the length requirement.",
		ChatAccessible:    false,
	},
	"add_or_trim": {
		Message: `First, call get_current_allocations() to retrieve my portfolio holdings and weights. Then use Google Search to find current news, recent earnings, analyst sentiment, and valuation context for my holdings. You may also call get_open_positions_with_cost_basis() to see unrealized gains/losses when evaluating trim candidates — a position deep in the green may warrant profit-taking, while one deep in the red raises tax-loss or recovery questions.

Put your reasoning in the ` + "`thinking`" + ` field: begin by listing every ticker alongside its full name exactly as provided in the portfolio data — do not paraphrase or infer names. Use this list as your reference throughout; if a search result describes a security whose name does not match the provided name for that ticker, discard it and search more specifically. Then evaluate each position's upside and downside case, weighting by current allocation size, and identify any that are already overrepresented relative to their risk/reward.

Then fill each field with fluent markdown prose:

- **` + "`add_weight`" + `**: Pick exactly three holdings from my portfolio with the most compelling case for putting more money in right now. For each, write 2–4 sentences covering: the core upside catalyst, why it complements the rest of the portfolio (diversification, factor tilt, or thematic fit), and any key risk to monitor. Lead each entry with the ticker in bold.

- **` + "`trim_or_avoid`" + `**: Pick exactly three holdings from my portfolio where the case for adding more money is weakest — either due to a credible downside story, stretched valuation, or because the position is already overrepresented relative to the rest of the portfolio. For each, write 2–4 sentences covering: the core concern, what conditions would make this thesis wrong, and whether this is a full exit candidate or just a "don't add" signal. Lead each entry with the ticker in bold.

A holding may appear in both lists if it represents a high-conviction asymmetric bet — compelling upside but with an equally credible downside that warrants caution before sizing up.`,
		SystemInstruction: "You are an expert equity analyst helping a client make capital allocation decisions within their existing portfolio. Ground your recommendations in current, real-world data from your search results. Be direct and opinionated — avoid hedging every sentence.",
		ForcedTool:        "get_current_allocations",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"add_weight",
			"trim_or_avoid",
		},
		SectionTitles: map[string]string{
			"add_weight":    "✅ Add Weight To",
			"trim_or_avoid": "✂️ Trim or Avoid",
		},
	},
	"general_analysis": {
		Message: `First, call get_current_allocations() to retrieve my portfolio holdings and weights. Then analyze my current portfolio given current market conditions.

What am I effectively betting on?

Put your reasoning in the ` + "`thinking`" + ` field: identify the major holdings, their primary sector exposure, and the current macroeconomic narratives surrounding them.

Then fill each field with fluent markdown prose:

- **` + "`macro_environment`" + `**: Briefly summarize the overarching market regime (e.g., inflationary, rate-cutting cycle, tech-led growth) and how this portfolio aligns with it.
- **` + "`sector_geographic_concentration`" + `**: Identify where my money is truly concentrated — do not just list percentages; group holdings by common themes like "AI infrastructure" or regional exposures like "US Tech versus European Industrials".
- **` + "`fama_french_factor_tilts`" + `**: Provide a qualitative, one-paragraph assessment using the Fama-French five-factor framework (Market, Size, Value/Growth, Profitability, Investment). Do not attempt precise calculations — reason from the holdings' known characteristics (e.g., mega-cap growth tech = strong negative HML, strong positive RMW; broad market ETFs = near-zero SMB). Conclude with one sentence on whether the combined tilt profile is deliberate or incidental.
- **` + "`implicit_bets`" + `**: Based on my concentration, what specific future events, market shifts, or currency dynamics am I effectively betting heavily on to happen? What am I most vulnerable to (e.g., exposed to a weakening USD)?
- **` + "`blind_spots`" + `**: What obvious market sectors, geographical regions, defensive assets, or FX hedges am I completely un-hedged against or missing out on entirely?`,
		ForcedTool:     "get_current_allocations",
		ChatAccessible: true,
		Cacheable:      true,
		SectionOrder: []string{
			"thinking",
			"macro_environment",
			"sector_geographic_concentration",
			"fama_french_factor_tilts",
			"implicit_bets",
			"blind_spots",
		},
		SectionTitles: map[string]string{
			"macro_environment":               "🌍 Macro Environment",
			"sector_geographic_concentration": "📊 Sector & Geographic Concentration",
			"fama_french_factor_tilts":        "🧮 Fama-French Factor Tilts",
			"implicit_bets":                   "🎯 Implicit Bets",
			"blind_spots":                     "🔍 Blind Spots & Under-allocations",
		},
	},
	"best_worst_scenarios": {
		Message: `First, call get_current_allocations() to retrieve my portfolio holdings and weights. Then analyze my current portfolio's exposure to market volatility.

Put your reasoning in the ` + "`thinking`" + ` field: map out specific, realistic macroeconomic and industry-specific catalysts that could drastically affect my major holdings.

Then fill each field with fluent markdown prose:

- **` + "`best_case`" + `**: Describe a highly favorable, yet realistic sequence of events over the next 6–12 months that would cause this portfolio to significantly outperform. Exactly which catalysts would need to align?
- **` + "`worst_case`" + `**: Describe a realistic stress scenario (e.g., specific regulatory shifts, supply chain shocks, currency headwinds, or rate changes) that would cause this portfolio to suffer heavy drawdowns. What is the structural weakness?
- **` + "`key_indicators`" + `**: List 2–3 specific, measurable macroeconomic or fundamental data points I should monitor closely to see which of the two scenarios is actively unfolding (e.g., upcoming inflation data, central bank meetings like the Fed or ECB, or key sector earnings).
- **` + "`historical_precedents`" + `**: Identify 2–3 specific historical periods (e.g., the 2000 dot-com bust, 2008 GFC, 2020 COVID crash, 2022 rate-hike cycle) where a portfolio with a similar geographic, sector, and asset-type composition faced comparable conditions. For each, briefly describe how such a portfolio would likely have performed — both during the drawdown and the subsequent recovery — and what the key driver of that outcome was.`,
		ForcedTool:     "get_current_allocations",
		ChatAccessible: true,
		Cacheable:      true,
		SectionOrder: []string{
			"thinking",
			"best_case",
			"worst_case",
			"key_indicators",
			"historical_precedents",
		},
		SectionTitles: map[string]string{
			"best_case":             "🌟 Best Case Scenario",
			"worst_case":            "⛈️ Worst Case Scenario",
			"key_indicators":        "📡 Key Indicators to Watch",
			"historical_precedents": "📜 Historical Precedents",
		},
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
		Message: `First, call get_risk_metrics() with the appropriate date range to retrieve the portfolio's statistical metrics. Then interpret the results in plain English, focusing on "The Story of the Money" rather than just the math.

Put your reasoning in the ` + "`thinking`" + ` field: break down each metric and what it implies about the investor's behavior and risk tolerance.

Then fill each field with fluent markdown prose, addressed directly to the client:

- **` + "`returns_narrative`" + `**: Explain the difference between the TWR and MWR. Am I a good "timer" of my own deposits/withdrawals, or is my behavior costing me money?
- **` + "`wealth_growth`" + `**: Based on the VAMI, how has the "purchasing power" of this portfolio changed?
- **` + "`efficiency_test`" + `**: Using Sharpe and Sortino, tell me if I'm taking "productive" risk or "reckless" risk. Is the "downside pain" (Sortino) significantly different from the "total swing" (Sharpe)?
- **` + "`stress_test`" + `**: Contextualize the Max Drawdown. Is this level of loss sustainable for a long-term investor?
- **` + "`investor_profile`" + `**: Is this portfolio more suited for the aggressive growth investor, defensive value-preservation investor, or neither?
- **` + "`verdict`" + `**: Is this a "smooth ride" or a "rollercoaster," and am I being rewarded for staying on it?`,
		SystemInstruction: "Act as a private wealth manager performing a year-end review for a client. Speak directly to the client.",
		ForcedTool:        "get_risk_metrics",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"returns_narrative",
			"wealth_growth",
			"efficiency_test",
			"stress_test",
			"investor_profile",
			"verdict",
		},
		SectionTitles: map[string]string{
			"returns_narrative": "The Returns Narrative",
			"wealth_growth":     "Wealth Growth",
			"efficiency_test":   "The Efficiency Test",
			"stress_test":       "The Stress Test",
			"investor_profile":  "Investor Profile",
			"verdict":           "The Verdict",
		},
	},
	"benchmark_analysis": {
		Message: `First, call get_benchmark_metrics() to compare my portfolio against the benchmark {benchmark}. Use the date range and risk-free rate appropriate to the user's request.

Then put your reasoning in the ` + "`thinking`" + ` field: compare the portfolio metrics against the benchmark's assumed baseline, evaluating the Alpha, Beta, and Tracking Error. Consider what combinations of these metrics imply (e.g., high Tracking Error + negative Alpha = poor active management).

Then provide a "so what?" analysis in each field:

- **` + "`manager_skill_vs_luck`" + `**: Based on the Alpha and Information Ratio, is the outperformance (if any) consistent or just a result of high active risk? Is the Alpha statistically meaningful?
- **` + "`risk_profile`" + `**: Use Beta and Treynor to explain if I am being properly compensated for the systematic risk I'm taking. Am I simply leveraging up market risk?
- **` + "`benchmarking`" + `**: Use Correlation and Tracking Error to tell me if this portfolio is a "closet indexer" or if it truly deviates from the benchmark in a meaningful way.
- **` + "`investor_profile`" + `**: Is this portfolio better suited for an aggressive growth investor or a defensive value-preservation investor expecting downside protection?
- **` + "`verdict`" + `**: Give me a blunt, executive summary of whether this portfolio is efficiently managed relative to the benchmark.`,
		SystemInstruction: "Act as an institutional portfolio analyst reviewing a fund manager's performance against a benchmark index.",
		ForcedTool:        "get_benchmark_metrics",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"manager_skill_vs_luck",
			"risk_profile",
			"benchmarking",
			"investor_profile",
			"verdict",
		},
		SectionTitles: map[string]string{
			"manager_skill_vs_luck": "Manager Skill vs. Luck",
			"risk_profile":          "Risk Profile",
			"benchmarking":          "Benchmarking",
			"investor_profile":      "Investor Profile",
			"verdict":               "Verdict",
		},
	},
	"upcoming_events": {
		Message: `Analyze the upcoming events that may impact my current portfolio over the next 30 days. Today's date is {current_date}.

First, use the Google Search tool to find scheduled earnings reports, upcoming macroeconomic data releases (e.g., inflation prints, central bank meetings like the Fed/ECB), and pertinent geopolitical events scheduled to occur within the next month that directly affect my major holdings. Use a <thinking> block to synthesize your findings, drop any events outside the 30-day window, and verify you have exact dates where possible.

Then fill each field with fluent markdown prose:
- **` + "`earnings_events`" + `**: List key upcoming earnings or shareholder meetings within the next 30 days for my holdings. Include specific dates. Explain briefly why each is critical.
- **` + "`macro_events`" + `**: Identify exact upcoming economic data releases or rate decisions occurring this month that could impact my asset allocation.
- **` + "`market_catalysts`" + `**: Highlight broader global events or ongoing developments reaching critical milestones this month that could influence my portfolio composition.
- **` + "`risks_opportunities`" + `**: Summarize the most significant short-term risks or opportunities based specifically on this 30-day calendar of events.`,
		SystemInstruction: "You are an expert financial analyst. Focus strictly on near-term calendar events (next 30 days). Discard any hypothetical scenarios or long-term trends. Always use exact dates when available.",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"earnings_events",
			"macro_events",
			"market_catalysts",
			"risks_opportunities",
		},
		SectionTitles: map[string]string{
			"earnings_events":     "📅 Scheduled Earnings & Corporate Events",
			"macro_events":        "🏦 Macroeconomic & Central Bank Events",
			"market_catalysts":    "🌍 Market & Geopolitical Catalysts",
			"risks_opportunities": "⚠️ Key Portfolio Risks & Opportunities",
		},
	},
	"geographic_sector_bottlenecks": {
		Message: `Analyze my Geographic & Sector Bottlenecks. 
First, call get_current_allocations() to retrieve my exact holdings. Then, meticulously loop through the major holdings using get_asset_fundamentals() to determine each asset's core country and sector breakdown.
Synthesize this underlying data in a <thinking> block to identify my true aggregate exposure.

Then fill each field with fluent markdown prose:
- **` + "`sector_overexposure`" + `**: Identify any sectors where I am heavily concentrated (e.g., Tech, Financials). Discuss what macroeconomic factors these sectors are most vulnerable to.
- **` + "`geographic_risks`" + `**: Identify the primary countries and regions where my money is tied up. What are the specific geopolitical or currency risks associated with this allocation?
- **` + "`mitigation`" + `**: Suggest broad themes or asset classes (not specific financial advice or new tickers) I could use to dilute these bottlenecks.`,
		SystemInstruction: "Act as an expert risk management analyst. Look through surface names into the underlying fundamentals.",
		ForcedTool:        "get_current_allocations",
		ChatAccessible:    true,
		Cacheable:         true,
		SectionOrder: []string{
			"thinking",
			"sector_overexposure",
			"geographic_risks",
			"mitigation",
		},
		SectionTitles: map[string]string{
			"sector_overexposure": "🏭 Sector Over-Exposure",
			"geographic_risks":    "🗺️ Geographic & Geopolitical Risks",
			"mitigation":          "⚖️ Recommendations for Mitigation",
		},
	},
	"biggest_drag_on_performance": {
		Message: `Identify the Biggest Drag on Performance in my portfolio.
First, call get_open_positions_with_cost_basis() to see which positions are currently operating at severe unrealized losses or flatlining relative to their cost basis. Also call get_benchmark_metrics() for at least one broad market benchmark (e.g. 'SPY' or 'VWCE.DE') to contextualize my overall portfolio performance.

In a <thinking> block, compare my underwater positions against the overall portfolio benchmark metrics (Alpha, Beta). Evaluate whether these losers are justifiable cyclical laggards or long-term structural failures.

Then fill each field with fluent markdown prose:
- **` + "`major_laggards`" + `**: List the 2-3 specific assets currently causing the most performance drag. Quote their average cost vs. current price.
- **` + "`the_why`" + `**: For each laggard, provide a brief analysis of *why* it's down. Is it a company-specific issue (bad earnings) or a sector-wide macro headwind? You may want to use Google Search to verify recent news.
- **` + "`tax_loss_context`" + `**: Without giving specific financial advice, present the tax-loss harvesting framework. Discuss what kind of alternative beta or factor tilt I could achieve by re-deploying this capital.`,
		SystemInstruction: "Act as a ruthless performance analyst parsing through a portfolio's weakest links.",
		ForcedTool:        "get_open_positions_with_cost_basis",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"major_laggards",
			"the_why",
			"tax_loss_context",
		},
		SectionTitles: map[string]string{
			"major_laggards":   "📉 The Major Laggards",
			"the_why":          "🧠 The 'Why'",
			"tax_loss_context": "🔄 Tax-Loss & Reallocation Context",
		},
	},
	"stress_test_beta": {
		Message: `Conduct a Stress Test vs. The Market (Beta Analysis).
First, call get_benchmark_metrics() against a global baseline (like 'SPY' or 'VWCE.DE'). Use the date range appropriate to the user's request.

In a <thinking> block, analyze my portfolio Beta. If my Beta is 1.5, I move 50% more violently than the market. Project what a sudden 15% market crash (typical of an interest rate shock or recession) would mathematically do to my portfolio. Identify from get_current_allocations() and get_asset_fundamentals() which holdings are providing the high beta vs the low beta.

Then fill each field with fluent markdown prose:
- **` + "`drawdown_scenario`" + `**: Based strictly on my portfolio Beta, quantify mechanically how a rapid 15% market drop would manifest in my total percentage drawdown. 
- **` + "`beta_contributors`" + `**: Identify which categories or specific holdings are likely supercharging my volatility, and which are acting as anchors holding my portfolio steady.
- **` + "`defensive_evaluation`" + `**: Assess if my portfolio contains adequate 'defensive' properties (e.g. bonds, utilities, cash) to survive a prolonged secular bear market, or if I am fundamentally positioned as a high-growth bull-market participant.`,
		SystemInstruction: "Act as an institutional risk manager conducting a scenario stress test.",
		ForcedTool:        "get_benchmark_metrics",
		ChatAccessible:    true,
		Cacheable:         false,
		SectionOrder: []string{
			"thinking",
			"drawdown_scenario",
			"beta_contributors",
			"defensive_evaluation",
		},
		SectionTitles: map[string]string{
			"drawdown_scenario":    "🌩️ The 15% Drawdown Scenario",
			"beta_contributors":    "🎢 Beta Contributors & Detractors",
			"defensive_evaluation": "🛡️ Defensive Evaluation",
		},
	},
	"risk_metrics_comparison": {
		Message: `Here are the risk and return metrics for two portfolios I want you to compare, for the period {from} to {to} (risk-free rate: {rfr}).

**Portfolio A — {name_a}:**
- TWR: {a_twr}
- MWR: {a_mwr}
- Sharpe Ratio: {a_sharpe}
- Sortino Ratio: {a_sortino}
- VAMI (growth of 1,000 invested at start): {a_vami}
- Annualised Volatility: {a_vol}
- Max Drawdown: {a_dd}

**Portfolio B — {name_b}:**
- TWR: {b_twr}
- MWR: {b_mwr}
- Sharpe Ratio: {b_sharpe}
- Sortino Ratio: {b_sortino}
- VAMI: {b_vami}
- Annualised Volatility: {b_vol}
- Max Drawdown: {b_dd}

In the ` + "`thinking`" + ` field, reason through what these numbers tell you — which portfolio delivered more return per unit of risk, which had shallower drawdowns, which compounded wealth faster, and what the spread between MWR and TWR signals about investment timing.

Then fill each field with fluent markdown prose, addressed directly to the investor:

- **` + "`returns_comparison`" + `**: Compare the raw TWR and MWR of both portfolios. Which came out ahead and by how much? Does the MWR–TWR spread reveal better or worse timing of cash flows in one versus the other?

- **` + "`risk_efficiency`" + `**: Which portfolio squeezed more return per unit of risk (Sharpe, Sortino)? Was the higher-return portfolio simply taking proportionately more downside risk, or was it genuinely more efficient?

- **` + "`wealth_growth`" + `**: Compare the VAMI figures and volatility. In practical terms, what is the gap in final wealth if 1,000 was invested at the start? How "bumpy" was each ride relative to its reward?

- **` + "`verdict`" + `**: Give a clear, jargon-free verdict: which portfolio is the better risk-adjusted bet, and what is the single most important reason why?`,
		SystemInstruction: "Act as a private wealth manager performing a comparative performance review for a client. Speak directly to the client. Focus on what the numbers mean in practical terms, not just what they are.",
		ChatAccessible:    true,
		Cacheable:         false,
		Schema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"thinking":           stringSchema(),
				"returns_comparison": stringSchema(),
				"risk_efficiency":    stringSchema(),
				"wealth_growth":      stringSchema(),
				"verdict":            stringSchema(),
			},
			Required: []string{"thinking", "returns_comparison", "risk_efficiency", "wealth_growth", "verdict"},
		},
		SectionOrder: []string{
			"thinking",
			"returns_comparison",
			"risk_efficiency",
			"wealth_growth",
			"verdict",
		},
		SectionTitles: map[string]string{
			"returns_comparison": "📈 Returns Comparison",
			"risk_efficiency":    "⚖️ Risk Efficiency",
			"wealth_growth":      "💰 Wealth Growth",
			"verdict":            "🏁 Verdict",
		},
	},
	"holdings_comparison": {
		Message: `Here are the current top holdings for two portfolios I want you to compare. Both portfolios span the period {from} to {to}.

**Portfolio A — {name_a}:**
{holdings_a}

**Portfolio B — {name_b}:**
{holdings_b}

In the ` + "`thinking`" + ` field, identify the major composition differences — different sectors, geographies, concentration levels, or asset types — and reason through how those structural differences might translate into different performance, risk, and future sensitivity.

Then fill each field with fluent markdown prose, addressed directly to the investor:

- **` + "`composition_differences`" + `**: What are the most significant structural differences between these two portfolios? Consider sectors, geographies, asset types, and diversification levels. Be specific about what is present in one but absent or underweight in the other.

- **` + "`performance_drivers`" + `**: Based on the composition differences, which specific bets in each portfolio are most likely driving divergent outcomes? Which holdings in one portfolio are doing work that the other portfolio lacks?

- **` + "`verdict`" + `**: Give a clear, jargon-free summary of the key trade-off between these two approaches. Which is more suitable for growth-oriented investors? Which for defensive, income-focused, or lower-volatility goals?`,
		SystemInstruction: "Act as an expert portfolio construction analyst comparing two investment portfolios. Speak directly to the client. Focus on the structural differences and their real-world implications.",
		ChatAccessible:    true,
		Cacheable:         false,
		Schema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"thinking":               stringSchema(),
				"composition_differences": stringSchema(),
				"performance_drivers":    stringSchema(),
				"verdict":                stringSchema(),
			},
			Required: []string{"thinking", "composition_differences", "performance_drivers", "verdict"},
		},
		SectionOrder: []string{
			"thinking",
			"composition_differences",
			"performance_drivers",
			"verdict",
		},
		SectionTitles: map[string]string{
			"composition_differences": "🧩 Composition Differences",
			"performance_drivers":     "🎯 Performance Drivers",
			"verdict":                 "🏁 Verdict",
		},
	},
}

// IsValidCannedType returns true if the specified type is a chat-accessible registered prompt.
func IsValidCannedType(promptType string) bool {
	cp, ok := CannedPrompts[promptType]
	return ok && cp.ChatAccessible
}
