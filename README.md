# Intro

Why even write another rebalancer? Usually, the only motivation is that the
existing software doesn't satisfy the developer. There may be different reasons
why improving the existing programs isn't something the dev wants. Maybe the
language is the problem or architecture. For me there were many reasons
including these. Two most advanced rebalancers for lnd are
[rebalance-lnd](https://github.com/C-Otto/rebalance-lnd) (Python) and
[bos](https://github.com/alexbosworth/balanceofsatoshis) (JS). However, each has
something the other one lacks, both have runtime requirements and both are
_slow_. I decided to fix these issues by rewriting the things I liked in Go
instead so it can be easily compiled and used anywhere. I also made the output
pretty, close to the design of [accumulator's fork of
rebalance-lnd](https://github.com/accumulator/rebalance-lnd).

![screenshot](screenshot.png)

# Features

- automatically pick source and target channel by local/remote liquidity ratio
- retry indefinitely until it succeeds or 6 hours pass (by default)
- payments time out after 5 minutes (by default) so if something's stuck the
  process will continue shortly
- timeouts can be customized
- JSON/TOML config file to set some defaults you prefer
- optional route probing using binary search to rebalance a smaller amount
- optional rapid rebalancing using the same route for further rebalances 
  unitl route is depleted in case a rebalance succeeds
- data caching to speed up alias resolution, quickly skip failing channel pairs
  etc.
- storing/loading cached nodes information to disk to "warm up" much faster next
  time you launch the program
- sensible node capacity formatting according to [Bitcoin
  design](https://bitcoin.design/guide/designing-products/units-and-symbols/)
  guidelines (easy to tell how many full coins there are)
- automatic max fee calculation from the target channel policy and preferred
  economy fee ratio (the amount spent on rebalance to the expected income from
  this channel)
- excluding your channels from consideration
- excluding any nodes from routing through (if they're known to be slow or constantly failing to route anything)
- using just one source and/or target channel (by default all imbalanced
  channels are considered and pairs are chosen randomly)
- calculate the rebalance amount automatically from current and desired balance
  percent
- safety precautions that prevent balances going beyond 50% of channel capacity,
  can be turned off explicitly if that's what you want
- saving successful rebalance parameters into a CSV file for further profit analysis
  with any external tools

# Installation

You need to have Go SDK installed, then simply run `go install
github.com/rkfg/regolancer@latest` and by default it will download, compile and
build the binary in `~/go/bin/regolancer`. To crosscompile for other platforms
use `GOARCH` and `GOOS` env vars to choose the target architecture and OS. For
RPi run it as `GOARCH=arm64 go install github.com/rkfg/regolancer@latest` if you
run a 64 bit system (and you should!). You'll find the binaries in
`~/go/bin/linux_arm64`. For 32 bit use `GOARCH=arm`, the binary will be located
in `~/go/bin/linux_arm`.

# Parameters

```
Config:
  -f, --config                 config file path          
                                                                                           
Node Connection:                                                
  -c, --connect                connect to lnd using host:port
  -t, --tlscert                path to tls.cert to connect
      --macaroon-dir           path to the macaroon directory
      --macaroon-filename      macaroon filename
  -n, --network                bitcoin network to use

Common:
      --pfrom                  channels with less than this inbound liquidity percentage will be considered as source channels
      --pto                    channels with less than this outbound liquidity percentage will be considered as target channels
  -p, --perc                   use this value as both pfrom and pto from above
  -a, --amount                 amount to rebalance
      --rel-amount-to          calculate amount as the target channel capacity fraction (for example, 0.2 means you want to achieve at most 20% target channel local balance)
      --rel-amount-from        calculate amount as the source channel capacity fraction (for example, 0.2 means you want to achieve at most 20% source channel remote balance)
  -b, --probe-steps            if the payment fails at the last hop try to probe lower amount using this many steps
      --allow-rapid-rebalance  if a rebalance succeeds the route will be used for further rebalances until criteria for channels is not satifsied
      --min-amount             if probing is enabled this will be the minimum amount to try 
  -i, --exclude-channel-in     don't use this channel as incoming (can be specified multiple times)
  -o, --exclude-channel-out    don't use this channel as outgoing (can be specified multiple times)
  -e, --exclude-channel        (DEPRECATED) don't use this channel at all (can be specified multiple times)
  -d, --exclude-node           (DEPRECATED) don't use this node for routing (can be specified multiple times)
      --exclude                don't use this node or your channel for routing (can be specified multiple times)
      --to                     try only this channel or node as target (should satisfy other constraints too; can be specified multiple times)
      --from                   try only this channel or node as source (should satisfy other constraints too; can be specified multiple times)
      --fail-tolerance         a payment that differs from the prior attempt by this ppm will be cancelled
      --allow-unbalance-from   (DEPRECATED) let the source channel go below 50% local liquidity, use if you want to drain a channel; you should also set --pfrom to >50
      --allow-unbalance-to     (DEPRECATED) let the target channel go above 50% local liquidity, use if you want to refill a channel; you should also set --pto to >50
  -r, --econ-ratio             economical ratio for fee limit calculation as a multiple of target channel fee (for example, 0.5 means you want to pay at max half the fee you might
                               earn for routing out of the target channel)
      --econ-ratio-max-ppm     limits the max fee ppm for a rebalance when using econ ratio 
  -F, --fee-limit-ppm          don't consider the target channel fee and use this max fee ppm instead (can rebalance at a loss, be careful)
  -l, --lost-profit            also consider the outbound channel fees when looking for profitable routes so that outbound_fee+inbound_fee < route_fee

Node Cache:
      --node-cache-filename    save and load other nodes information to this file, improves cold start performance
      --node-cache-lifetime    nodes with last update older than this time (in minutes) will be removed from cache after loading it
      --node-cache-info        show red and cyan 'x' characters in routes to indicate node cache misses and hits respectively

Timeouts:
      --timeout-rebalance      max rebalance session time in minutes
      --timeout-attempt        max attempt time in minutes
      --timeout-info           max general info query time (local channels, node id etc.) in seconds
      --timeout-route          max channel selection and route query time in seconds

Others:
  -s, --stat                   save successful rebalance information to the specified CSV file
  -v, --version                show program version and exit
      --info                   show rebalance information
  -h, --help                   Show this help message
```

Look in `config.json.sample` or `config.toml.sample` for corresponding keys,
they're not exactly equivalent. If in doubt, open `main.go` and look at the `var
params struct`. If defined in both config and CLI, the CLI parameters take
priority. Connect, macaroon and tls settings can be omitted if you have a
default `lnd` installation.

# Node cache

Enable the cache by setting `--node-cache-filename=/path/to/cache.dat` (or
`node_cache_filename` config parameter), you're free to choose any path and file
name you like. It speeds up printing routes and lowers load on lnd in case you
run multiple regolancer instances. If you're not interested in technical
details, feel free to skip the following section.

## How this cache works

Node cache is only used for printing routes, it contains basic node information
such as alias, total capacity, number of channels, features etc. However,
getting this information might be slow as every request to lnd is processed
sequentially. The first few routes print noticeably slower until more nodes
"around" you are queried and cached in RAM. This information shouldn't be very
up-to-date (unlike the channel balances, policies etc. which are retrieved on
every launch and are only cached for the run time) and nodes themselves
broadcast updates not very often. It makes sense to persist this data to disk
and load it on every run so that routes are printed almost instantly, and the
payment is only attempted after the route is fully printed. It would be good to
run a payment attempt and route print in parallel but currently the payment
function can dump errors and it would interfere with the route output.

However, there's a gotcha that I learned from other users of regolancer: people
run multiple instances of it in parallel, so they might terminate at different
times. If all those instances use the same cache file, they will overwrite it
and lose information that another instance might have stored before them, or
they might start writing at the same time and corrupt it. One way to solve it is
to use separate cache files but then each instance would query lnd for the same
nodes as other instances. So instead of this I added file locking (using
`/tmp/regolancer.lock` file on Linux and probably `%tmpdir%/regolancer.lock` on
Windows, haven't tested) that allows multiple readers but just one writer and
implemented simple cache merging. When it's saved, first we load the existing
cache (under a write lock so no one can access it), copy all nodes that are
missing in our own cache or have a more recent update time, then save the result
replacing the cache file.

There's also the cache expiration parameter (`--node-cache-lifetime`) set to
1440min/24h by default that lets you skip cached nodes that are older than that.
It doesn't affect any actual logic, just that these nodes will be queried again
from lnd when they're printed for the first time. Set it to a bigger number if
you don't care about the node stat actuality.

Cache is also saved if you interrupt regolancer with Ctrl+C.

# Probing

This is an obscure feature that `bos` uses in rebalances, it relies on protocol
error messages. I didn't read the `bos` source but figured out how to check if
the route can process the requested amount without actually making a payment:
generate a random payment hash and send it. `lnd` will refuse to accept it
(because there's no corresponding invoice) but the program gets a different
error than the usual `TEMPORARY_CHANNEL_FAILURE`. Then we can do a binary search
to estimate the best amount in a few steps. Note, however, that the smallest
amount can be 2<sup>n</sup> times less than you planned to rebalance (where `n`
is the number of steps during probing). For example, 5 steps and 1,000,000 sats
amount mean that you might rebalance at least 1000000/2<sup>5</sup> = 31250 sats
if the probe succeeds. You can override this minimum with `--min-amount` so that
probing begins with this amount instead and either goes up or fails immediately.
Another problem is that fees can become too high for smaller amounts because of
the base fee that starts dominating the fee structure. It's handled properly,
however.

When enabled, probing starts if the payment fails at the second to last channel.
The last channel comes to yourself so you know it's guaranteed to accept the
specified amount. If all other channels could route this amount, the only
unknown one is that second to last. Then we try different amounts until either a
good amount is found or we run out of steps. If a good amount is learned the
payment is then done along this route and it should succeed. If, for whatever
reason, it doesn't (liquidity shifted somewhere unexpectedly) the cycle
continues.

# What's wrong with the other rebalancers

While I liked probing in `bos`, it has many downsides: gives up quickly on
trivial errors, has very rigid channel selection options (all should be chosen
manually), no automatic fee calculation, cryptic errors, [weird
defaults](https://github.com/alexbosworth/balanceofsatoshis/issues/88) and
[ineffective
design](https://github.com/alexbosworth/balanceofsatoshis/issues/125). It can
also unbalance another channel, there are no safety belts. It might be okay if
you absolutely need one channel to be refilled no matter the cost but if you
want your node to be profitable you have to account for every sat.

Rebalance-lnd is much better for automation but it still can't choose multiple
source and destination channels and try to send between them. You have to select
one source and/or one target, the other side is chosen randomly, often only to
discard a lot of routes because of too high fees (this constraint can be
specified while querying routes but it isn't). The default route limit is 100
so you either have to increase it or restart the script until it succeeds. I
noticed multiple times that it concedes after 20-30 attempts saying there are no
more routes but after restart still finds and tries more. It also lacks probing,
consumes quite a lot of CPU time sometimes and I personally find Python a big
pain to work with.

# Why rebalance at all

I'm still a bit torn on this topic. At some points in time I was a fan of
rebalancing, then I stopped, now I began again. I guess it all comes with
experience. LN is still in its infancy and when the network is widely available
and used on daily basis rebalancing won't be needed. But today we have a lot of
poorly managed nodes (especially the big ones!) with default minimal fees and
more experienced nodes quickly drain this liquidity only to resell it for a
higher price. If a node has hundreds or thousands of channels with zero
liquidity hints it becomes very hard to balance such channels. It essentially
boils down to a bruteforce which is exactly what this program does. It seeks the
network for liquidity that's cheaper than your own and moves it to you.

For now some liquidity can be just dead. Even if you set 0/0 on a full channel
you see no routing through it. Because it's the opposite direction that everyone
wants. So you have to move it manually, getting incoming liquidity to sell for
some other outbound liquidity you have. And when you run out of it you need to
refill the channels using that dead liquidity. In the future, hopefully, the
daily network activity will do this job thanks to circular economy. Today it's
not yet the case.

However, there's not much point in rebalancing all the channels. See which are
empty for weeks and consider them as candidates. From my experience, you might
have a few channels that can be drained very quickly if the fee is too low. They
are channels to exchanges and service providers, sometimes other big nodes that
consume all liquidity you throw at them. That's your source of income,
basically. These channels should be added to exceptions in your config so
they're never used as a source, even when they match the percent limit.

# How to route better

By all means, use [charge-lnd](https://github.com/accumulator/charge-lnd). Your
goal is to minimize local forward failures. It can be achieved with fees and/or
max HTLC parameter. You can try to move the dead liquidity with 0/0 fee before
doing rebalance. You absolutely should discourage routing through empty
channels. Best way is to set max_htlc on them so they're automatically discarded
during route construction. You can also disable them (it only happens on your
end so you'll be able to receive liquidity but not send it) but it hurts your
score on various sites so better not to do it. Increase fees or lower max_htlc and
you'll be good. You can set multiple brackets with multiple limits like:
- 20% local balance => set max_htlc to 0.1 of channel capacity (so it can
  process ≈2 payments max or more smaller payments)
- 10% local balance => set max_htlc to 0.01 of channel capacity (small payments
  can get through but channel won't be drained quickly)
- 1% local balance => set max_htlc to 1 sat essentially disabling it

Same can be done with fees but if you decide to rebalance, watch out: you might
spend a lot on rebalancing if your empty channel sets 5000ppm fee but after it
gets refilled it switches back to regular 50 or 100ppm. You'll never earn that
back. Learn how `charge-lnd` works and write your own rules!

# Goals and future

It's a small weekend project that I did for myself and my own goals. I gladly
accept contributions and suggestions though! For now I implemented almost
everything I needed, maybe except a couple of timeouts being configurable. But I
don't see much need for that as of now. The main goals and motivation for this
project were:
- make it reliable and robust so I don't have to babysit it (stop/restart if it
  hangs, crashes or gives up early)
- make it fast and lightweight, don't stress `lnd` too much as it all should run
  on RPi
- provide many settings for tweaking, every node is different but the incentives
  are the same
- since it's a user-oriented software, make the output pleasant to look at, the
  important bits should be highlighted and easy to read

# Feedback and contact

We have a Matrix room to discuss this program and solve issues, feel free to
join [#regolancer:matrix.org](https://matrix.to/#/#regolancer:matrix.org)!