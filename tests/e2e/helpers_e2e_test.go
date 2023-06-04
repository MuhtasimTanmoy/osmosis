package e2e

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"strconv"
	"time"
	"github.com/osmosis-labs/osmosis/osmomath"

	appparams "github.com/osmosis-labs/osmosis/v16/app/params"
	"github.com/osmosis-labs/osmosis/v16/tests/e2e/configurer/chain"
	"github.com/osmosis-labs/osmosis/v16/tests/e2e/configurer/config"
	"github.com/osmosis-labs/osmosis/v16/tests/e2e/initialization"
	"github.com/osmosis-labs/osmosis/v16/x/concentrated-liquidity/model"
	"github.com/osmosis-labs/osmosis/v16/x/concentrated-liquidity/types"
	gammtypes "github.com/osmosis-labs/osmosis/v16/x/gamm/types"
	lockuptypes "github.com/osmosis-labs/osmosis/v16/x/lockup/types"
	superfluidtypes "github.com/osmosis-labs/osmosis/v16/x/superfluid/types"
	cltypes "github.com/osmosis-labs/osmosis/v16/x/concentrated-liquidity/types"
)

var defaultFeePerTx = sdk.NewInt(1000)

var (
	denom0       string = "stake"
	denom1       string = "uosmo"
	tickSpacing  uint64 = 100
	spreadFactor        = "0.001" // 0.1%
)

// calculateSpreadRewardGrowthGlobal calculates spread reward growth global per unit of virtual liquidity based on swap parameters:
// amountIn - amount being swapped
// spreadFactor - pool's spread factor
// poolLiquidity - current pool liquidity
func calculateSpreadRewardGrowthGlobal(amountIn, spreadFactor, poolLiquidity sdk.Dec) sdk.Dec {
	// First we get total spread reward charge for the swap (ΔY * spreadFactor)
	spreadRewardChargeTotal := amountIn.Mul(spreadFactor)

	// Calculating spread reward growth global (dividing by pool liquidity to find spread reward growth per unit of virtual liquidity)
	spreadRewardGrowthGlobal := spreadRewardChargeTotal.Quo(poolLiquidity)
	return spreadRewardGrowthGlobal
}

// calculateSpreadRewardGrowthInside calculates spread reward growth inside range per unit of virtual liquidity
// spreadRewardGrowthGlobal - global spread reward growth per unit of virtual liquidity
// spreadRewardGrowthBelow - spread reward growth below lower tick
// spreadRewardGrowthAbove - spread reward growth above upper tick
// Formula: spreadRewardGrowthGlobal - spreadRewardGrowthBelowLowerTick - spreadRewardGrowthAboveUpperTick
func calculateSpreadRewardGrowthInside(spreadRewardGrowthGlobal, spreadRewardGrowthBelow, spreadRewardGrowthAbove sdk.Dec) sdk.Dec {
	return spreadRewardGrowthGlobal.Sub(spreadRewardGrowthBelow).Sub(spreadRewardGrowthAbove)
}

// Assert balances that are not affected by swap:
// * same amount of `stake` in balancesBefore and balancesAfter
// * amount of `e2e-default-feetoken` dropped by 1000 (default amount for fee per tx)
// * depending on `assertUosmoBalanceIsConstant` and `assertUionBalanceIsConstant` parameters, check that those balances have also not been changed
func (s *IntegrationTestSuite) assertBalancesInvariants(balancesBefore, balancesAfter sdk.Coins, assertUosmoBalanceIsConstant, assertUionBalanceIsConstant bool) {
	s.Require().True(balancesAfter.AmountOf("stake").Equal(balancesBefore.AmountOf("stake")))
	s.Require().True(balancesAfter.AmountOf("e2e-default-feetoken").Equal(balancesBefore.AmountOf("e2e-default-feetoken").Sub(defaultFeePerTx)))
	if assertUionBalanceIsConstant {
		s.Require().True(balancesAfter.AmountOf("uion").Equal(balancesBefore.AmountOf("uion")))
	}
	if assertUosmoBalanceIsConstant {
		s.Require().True(balancesAfter.AmountOf("uosmo").Equal(balancesBefore.AmountOf("uosmo")))
	}
}

// Get balances for address
func (s *IntegrationTestSuite) addrBalance(node *chain.NodeConfig, address string) sdk.Coins {
	addrBalances, err := node.QueryBalances(address)
	s.Require().NoError(err)
	return addrBalances
}

// Helper function for calculating uncollected spread rewards since the time that spreadRewardGrowthInsideLast corresponds to
// positionLiquidity - current position liquidity
// spreadRewardGrowthBelow - spread reward growth below lower tick
// spreadRewardGrowthAbove - spread reward growth above upper tick
// spreadRewardGrowthInsideLast - amount of spread reward growth inside range at the time from which we want to calculate the amount of uncollected spread rewards
// spreadRewardGrowthGlobal - variable for tracking global spread reward growth
func calculateUncollectedSpreadRewards(positionLiquidity, spreadRewardGrowthBelow, spreadRewardGrowthAbove, spreadRewardGrowthInsideLast sdk.Dec, spreadRewardGrowthGlobal sdk.Dec) sdk.Dec {
	// Calculating spread reward growth inside range [-1200; 400]
	spreadRewardGrowthInside := calculateSpreadRewardGrowthInside(spreadRewardGrowthGlobal, spreadRewardGrowthBelow, spreadRewardGrowthAbove)

	// Calculating uncollected spread rewards
	// Formula for finding uncollected spread rewards in time range [t1; t2]:
	// F_u = position_liquidity * (spread_rewards_growth_inside_t2 - spread_rewards_growth_inside_t1).
	spreadRewardsUncollected := positionLiquidity.Mul(spreadRewardGrowthInside.Sub(spreadRewardGrowthInsideLast))

	return spreadRewardsUncollected
}

// Get current (updated) pool
func (s *IntegrationTestSuite) updatedConcentratedPool(node *chain.NodeConfig, poolId uint64) types.ConcentratedPoolExtension {
	concentratedPool, err := node.QueryConcentratedPool(poolId)
	s.Require().NoError(err)
	return concentratedPool
}

func (s *IntegrationTestSuite) updatedCFMMPool(node *chain.NodeConfig, poolId uint64) gammtypes.CFMMPoolI {
	cfmmPool, err := node.QueryCFMMPool(poolId)
	s.Require().NoError(err)
	return cfmmPool
}

// Assert returned positions:
func (s *IntegrationTestSuite) validateCLPosition(position model.Position, poolId uint64, lowerTick, upperTick int64) {
	s.Require().Equal(position.PoolId, poolId)
	s.Require().Equal(position.LowerTick, lowerTick)
	s.Require().Equal(position.UpperTick, upperTick)
}

func (s *IntegrationTestSuite) validateMigrateResult(
	node *chain.NodeConfig,
	positionId, balancerPooId, poolIdLeaving, clPoolId, poolIdEntering uint64,
	percentOfSharesToMigrate, liquidityMigrated sdk.Dec,
	joinPoolAmt sdk.Coins,
	amount0, amount1 sdk.Int,
) {
	// Check that the concentrated liquidity match what we expect
	position := node.QueryPositionById(positionId)
	s.Require().Equal(liquidityMigrated, position.Liquidity)

	// Expect the poolIdLeaving to be the balancer pool id
	// Expect the poolIdEntering to be the concentrated liquidity pool id
	s.Require().Equal(balancerPooId, poolIdLeaving)
	s.Require().Equal(clPoolId, poolIdEntering)

	// exitPool has rounding difference.
	// We test if correct amt has been exited and frozen by comparing with rounding tolerance.
	defaultErrorTolerance := osmomath.ErrTolerance{
		AdditiveTolerance: sdk.NewDec(2),
		RoundingDir:       osmomath.RoundDown,
	}
	s.Require().Equal(0, defaultErrorTolerance.Compare(joinPoolAmt.AmountOf("stake").ToDec().Mul(percentOfSharesToMigrate).RoundInt(), amount0))
	s.Require().Equal(0, defaultErrorTolerance.Compare(joinPoolAmt.AmountOf("uosmo").ToDec().Mul(percentOfSharesToMigrate).RoundInt(), amount1))
}

func (s *IntegrationTestSuite) createFullRangePosition(node *chain.NodeConfig, from sdk.AccAddress, tokens sdk.Coins, poolId uint64) uint64 {
	// Check that exactly two coins are provided.
	if len(tokens) != 2 {
		return 0
	}

	concentratedPool := s.updatedConcentratedPool(node, poolId)

	// Defense in depth, ensure coins provided match the pool's token denominations.
	if tokens.AmountOf(concentratedPool.GetToken0()).LTE(sdk.ZeroInt()) {
		return 0
	}

	if tokens.AmountOf(concentratedPool.GetToken1()).LTE(sdk.ZeroInt()) {
		return 0
	}

	// Create a full range (min to max tick) concentrated liquidity position.
	positionId := node.CreateConcentratedPosition(from.String(), fmt.Sprintf("[%d]", cltypes.MinTick), fmt.Sprintf("%d", cltypes.MaxTick), tokens.String(), 0, 0, poolId)

	return positionId
}

func (s *IntegrationTestSuite) setupMigrationTest(
	chain *chain.Config,
	superfluidDelegated, superfluidUndelegating, unlocking, noLock bool,
	percentOfSharesToMigrate sdk.Dec,
) (joinPoolAmt sdk.Coins, balancerIntermediaryAcc superfluidtypes.SuperfluidIntermediaryAccount, balancerLock *lockuptypes.PeriodLock, poolCreateAcc, poolJoinAcc sdk.AccAddress, balancerPooId, clPoolId uint64, balancerPoolShareOut sdk.Coin, valAddr sdk.ValAddress) {

	node, err := chain.GetDefaultNode()
	s.NoError(err)

	fullRangeCoins := sdk.NewCoins(
		sdk.NewInt64Coin("uosmo",500000),
		sdk.NewInt64Coin("stake",500000),
	)

	fundTokens := []string{"50000000000uosmo", "50000000000stake"}
	poolJoinAddress := node.CreateWalletAndFund("poolJoinAddress", fundTokens)
	poolJoinAcc, err = sdk.AccAddressFromBech32(poolJoinAddress)
	s.Require().NoError(err)

	// fullRangeCoins := sdk.NewCoin()
	balancerPooId = node.CreateBalancerPool("nativeDenomPoolMigration.json", node.PublicAddress)
	// balancerPool := s.updatedCFMMPool(node, balancePoolId)

	balanceBeforeJoin := s.addrBalance(node, poolJoinAddress)
	node.JoinPoolNoSwap(poolJoinAddress, balancerPooId, gammtypes.OneShare.MulRaw(50).String(), sdk.Coins{}.String())
	balanceAfterJoin := s.addrBalance(node, poolJoinAddress)

	// The balancer join pool amount is the difference between the account balance before and after joining the pool.
	joinPoolAmt, _ = balanceBeforeJoin.SafeSub(balanceAfterJoin)

	// Determine the balancer pool's LP token denomination.
	balancerPoolDenom := gammtypes.GetPoolShareDenom(balancerPooId)

	// Register the balancer pool's LP token as a superfluid asset
	chain.EnableSuperfluidAsset(balancerPoolDenom)

	// Note how much of the balancer pool's LP token the account that joined the pool has.
	balanceCurrent := s.addrBalance(node, poolJoinAddress)

	balancerPoolShareOut = sdk.Coin{
		Amount: balanceCurrent.AmountOf(balancerPoolDenom),
		Denom:  balancerPoolDenom,
	}

	clPoolId, err = node.CreateConcentratedPool(poolJoinAddress, denom0, denom1, tickSpacing, spreadFactor)
	// clPool := s.updatedConcentratedPool(node, clPoolId)

	record := strconv.FormatUint(balancerPooId, 10) + "," + strconv.FormatUint(clPoolId, 10)
	node.SubmitReplaceMigrationRecordsProposal(record, sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(config.InitialMinDeposit)))
	chain.LatestProposalNumber += 1
	node.DepositProposal(chain.LatestProposalNumber, false)
	totalTimeChan := make(chan time.Duration, 1)
	go node.QueryPropStatusTimed(chain.LatestProposalNumber, "PROPOSAL_STATUS_PASSED", totalTimeChan)
	for _, node := range chain.NodeConfigs {
		node.VoteYesProposal(initialization.ValidatorWalletName, chain.LatestProposalNumber)
	}

	// if querying proposal takes longer than timeoutPeriod, stop the goroutine and error
	timeoutPeriod := 2 * time.Minute
	select {
	case <-time.After(timeoutPeriod):
		err := fmt.Errorf("go routine took longer than %s", timeoutPeriod)
		s.Require().NoError(err)
	case <-totalTimeChan:
		// The goroutine finished before the timeout period, continue execution.
	}

	// The unbonding duration is the same as the staking module's unbonding duration.
	// hardcore this, data get from config file
	unbondingDuration := time.Duration(240000000000)

	// Lock the LP tokens for the duration of the unbonding period.
	originalGammLockId := uint64(0)
	if !noLock {
		// TODO: get originalGammLockId return from LockTokens cmd below
		// lock tokens
		node.LockTokens(fmt.Sprintf("%v%s", balancerPoolShareOut.Amount, balancerPoolShareOut.Denom), unbondingDuration.String(), poolJoinAcc.String())
		chain.LatestLockNumber += 1
		// add to existing lock
		node.AddToExistingLock(balancerPoolShareOut.Amount, balancerPoolShareOut.Denom, unbondingDuration.String(), poolJoinAcc.String())
		// originalGammLockId = s.LockTokens(poolJoinAcc, sdk.NewCoins(balancerPoolShareOut), unbondingDuration)
	}

	// Superfluid delegate the balancer lock if the case requires it.
	// Note the intermediary account that was created.
	if superfluidDelegated {
		node.SuperfluidDelegate(int(originalGammLockId), chain.NodeConfigs[1].OperatorAddress, poolJoinAcc.String())
		connectedIntermediaryAccount := node.QueryConnectedIntermediaryAccount(fmt.Sprintf("%d", originalGammLockId))
		balancerIntermediaryAcc = superfluidtypes.SuperfluidIntermediaryAccount{
			Denom:   connectedIntermediaryAccount.Denom,
			ValAddr: connectedIntermediaryAccount.ValAddr,
			GaugeId: connectedIntermediaryAccount.GaugeId,
		}
	}

	// Superfluid undelegate the lock if the test case requires it.
	if superfluidUndelegating {
		node.SuperfluidUndelegate(int(originalGammLockId), poolJoinAcc.String())
	}

	// Unlock the balancer lock if the test case requires it.
	if unlocking {
		if superfluidUndelegating {
			node.SuperfluidUnbondLock(int(originalGammLockId), poolJoinAcc.String())
		} else {
			lock := node.QueryLockedById(fmt.Sprintf("%d", originalGammLockId))
			node.LockupBeginUnlock(int(originalGammLockId), lock.Coins.String(), node.PublicAddress)
		}
	}

	balancerLock = &lockuptypes.PeriodLock{}
	if !noLock {
		balancerLock = node.QueryLockedById(fmt.Sprintf("%d", originalGammLockId))
	}
	
	// Create a full range position in the concentrated liquidity pool.
	s.createFullRangePosition(node, sdk.MustAccAddressFromBech32(poolJoinAddress), fullRangeCoins, clPoolId)

	// Register the CL full range LP tokens as a superfluid asset.
	clPoolDenom := fmt.Sprintf("%s/%d", cltypes.ConcentratedLiquidityTokenPrefix, clPoolId)
	chain.EnableSuperfluidAsset(clPoolDenom)

	return joinPoolAmt, balancerIntermediaryAcc, balancerLock, poolCreateAcc, poolJoinAcc, balancerPooId, clPoolId, balancerPoolShareOut, valAddr
}
