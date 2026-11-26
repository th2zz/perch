package dev.perch.console

import dev.perch.console.core.format.UsageSummary
import dev.perch.console.core.herdrclient.UsageFacts
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UsageSummaryTest {

    private val hisSession = UsageFacts(
        harness = "codex",
        model = "deepseek-flash",
        effort = "high",
        contextUsed = 654_200,
        contextWindow = 996_147,
        totalTokens = 1_541_443_593,
        cachedTokens = 1_534_388_599,
        access = "full access, no approvals asked",
        goal = "build the mvp",
        goalStatus = "blocked",
    )

    @Test
    fun hisOwnSessionReadsAsAWholePanel() {
        assertEquals(
            listOf(
                UsageSummary.Row("Model", "deepseek-flash · high effort"),
                UsageSummary.Row("Context", "65% · 654k of 996k"),
                UsageSummary.Row("Spent", "7.05M not cached · 1.54B total (99% cached)"),
                UsageSummary.Row("Access", "full access, no approvals asked"),
                UsageSummary.Row("Goal", "blocked — build the mvp"),
            ),
            UsageSummary.rows(hisSession),
        )
    }

    @Test
    fun aHarnessThatNamesNoWindowGetsNoPercentage() {
        val claude = UsageFacts(
            harness = "claude",
            model = "deepseek-flash",
            contextUsed = 15_639,
            totalTokens = 96_000,
        )

        assertEquals(
            listOf(
                UsageSummary.Row("Model", "deepseek-flash"),
                UsageSummary.Row("Context", "16k used"),
                UsageSummary.Row("Spent", "96k not cached · 96k total (0% cached)"),
            ),
            UsageSummary.rows(claude),
        )
    }

    @Test
    fun aSilentHarnessProducesNoRowsAtAll() {
        assertTrue(UsageSummary.rows(UsageFacts()).isEmpty())
        assertTrue(UsageFacts().isEmpty)
    }

    @Test
    fun anEmptyPanelStillSaysWhichKindOfEmptyItIs() {
        assertEquals(
            UsageSummary.Panel.NotReadable,
            UsageSummary.panel(UsageFacts(harness = "gemini", readable = false)),
        )
        assertEquals(
            UsageSummary.Panel.NothingYet,
            UsageSummary.panel(UsageFacts(harness = "codex", readable = true)),
        )
        assertTrue(UsageSummary.panel(hisSession) is UsageSummary.Panel.Rows)
    }

    @Test
    fun countsAreShortEnoughForAPhoneAndStillExactBelowAThousand() {
        assertEquals("0", UsageSummary.tokens(0))
        assertEquals("999", UsageSummary.tokens(999))
        assertEquals("1k", UsageSummary.tokens(1_000))
        assertEquals("996k", UsageSummary.tokens(996_147))
        assertEquals("7.05M", UsageSummary.tokens(7_054_994))
        assertEquals("1.54B", UsageSummary.tokens(1_541_443_593))
    }
}
