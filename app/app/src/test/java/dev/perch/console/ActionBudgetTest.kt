package dev.perch.console

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.features.queue.DecisionActions
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * "At most three things to do on a screen" (spec SC-010), made checkable.
 *
 * The budget is not enforced by counting buttons at runtime — it is enforced by a
 * structure: a card offers what the computer said is possible (approve, reject, or
 * answer — never all three, and the key table has no rule with both approve and
 * answer), and the escape route appears *only* when none of them is available. This
 * test pins the exclusive-or, because that is the part a future edit can break by
 * making the escape route unconditional "for discoverability" — and then the busiest
 * card on the screen has four actions instead of three.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class ActionBudgetTest {

    @get:Rule
    val compose = createComposeRule()

    private fun decision(
        approvable: Boolean = false,
        rejectable: Boolean = false,
        answerable: Boolean = false,
    ) = Decision(
        id = "wA:p1@1",
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agentKind = "codex",
        promptText = "Do you want to proceed?",
        approvable = approvable,
        rejectable = rejectable,
        answerable = answerable,
        unmappableReason = "detection rule is not one Perch can drive",
    )

    private fun show(decision: Decision) {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                DecisionActions(
                    decision = decision,
                    busy = false,
                    onDecide = { },
                    onAnswer = { },
                    onOpenSession = { },
                )
            }
        }
    }

    @Test
    fun theEscapeRouteAppearsExactlyWhenNothingElseCan() {
        // The rule, stated over the whole space of flags the computer can send.
        for (approvable in listOf(false, true)) {
            for (rejectable in listOf(false, true)) {
                for (answerable in listOf(false, true)) {
                    val d = decision(approvable, rejectable, answerable)
                    val somethingToDo = approvable || rejectable || answerable
                    assertTrue(
                        "needsComputer must be the exact complement of having an action " +
                            "(approvable=$approvable rejectable=$rejectable answerable=$answerable)",
                        d.needsComputer == !somethingToDo,
                    )
                }
            }
        }
    }

    @Test
    fun aCardThatCanBeActedOnOffersNoEscapeRoute() {
        show(decision(approvable = true, rejectable = true))
        compose.onNodeWithText("Approve").assertExists()
        compose.onNodeWithText("Reject").assertExists()
        // Two buttons and nothing else: the third slot stays free for the case that
        // needs it, and a user with a working button is not invited to go and read a
        // terminal instead.
        compose.onAllNodesWithText("Open the session").assertCountEquals(0)
        compose.onAllNodesWithText("Answer").assertCountEquals(0)
    }

    @Test
    fun aCardNothingCanBeDoneWithOffersExactlyTheWayOut() {
        show(decision())
        compose.onAllNodesWithText("Approve").assertCountEquals(0)
        compose.onAllNodesWithText("Reject").assertCountEquals(0)
        compose.onAllNodesWithText("Answer").assertCountEquals(0)
        compose.onNodeWithText("Open the session").assertExists()
        // And it says why, so "we do not guess" does not read as "you are stuck".
        compose.onNodeWithText("Perch cannot press this one for you").assertExists()
    }

    @Test
    fun answeringIsOfferedAlone() {
        show(decision(answerable = true))
        compose.onNodeWithText("Answer").assertExists()
        compose.onAllNodesWithText("Approve").assertCountEquals(0)
        compose.onAllNodesWithText("Open the session").assertCountEquals(0)
    }
}
