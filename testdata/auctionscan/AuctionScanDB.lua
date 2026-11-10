AuctionScanDB = {
	["v"] = 3,
	["exchange"] = 1,
	["born"] = "6adb2f80-1a2b",
	["maxScans"] = 0,
	["seq"] = 7,
	["scans"] = {
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792900000|1",
			["t"] = 1792900000,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "all",
			["total"] = 4,
			["secs"] = 41,
			["expect"] = 4,
			["miss"] = 0,
			["lots"] = {
				"36919:0:1:950000:1000000:0:4:Вивалякуба", -- [1]
				"36919:0:1:950000:1000000:0:4:Вивалякуба", -- [2]
				"44000:-39:1:120000:150000:125000:2:Страхнул", -- [3]
				"34054:0:20:20000:0:0:1:", -- [4]
			},
		}, -- [1]
		{
			["id"] = "WoW Circle 3.3.5a x1|Horde|1792900600|2",
			["t"] = 1792900600,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Horde",
			["mode"] = "pages",
			["total"] = 2,
			["secs"] = 380,
			["expect"] = 3,
			["miss"] = 1,
			["resumes"] = 1,
			["lots"] = {
				"36919:0:2:1900000:2000000:0:3:Nadzh", -- [1]
				"43102:0:1:400:500:0:1:Ordo", -- [2]
			},
		}, -- [2]
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792901200|3",
			["t"] = 1792901200,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "pages",
			["total"] = 2,
			["secs"] = 2,
			["expect"] = 2,
			["miss"] = 0,
			["scope"] = "item",
			["itemID"] = 34054,
			["query"] = "Абсолютная пыль",
			["classIndex"] = 6,
			["subClassIndex"] = 7,
			["lots"] = {
				"34054:0:20:20000:22000:0:4:Вивалякуба", -- [1]
				"34054:0:5:6000:0:0:2:Страхнул", -- [2]
			},
		}, -- [3]
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792901800|4",
			["t"] = 1792901800,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "pages",
			["total"] = 1,
			["secs"] = 1,
			["expect"] = 1,
			["miss"] = 0,
			["scope"] = "query",
			["query"] = "кольцо",
			["lots"] = {
				"43250:0:1:500000:600000:0:4:Nadzh", -- [1]
			},
		}, -- [4]
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792902400|5",
			["t"] = 1792902400,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "pages",
			["total"] = 2,
			["secs"] = 3,
			["expect"] = 2,
			["miss"] = 0,
			["scope"] = "family",
			["family"] = 36932,
			["query"] = "Темный нефрит",
			["classIndex"] = 10,
			["subClassIndex"] = 5,
			["complete"] = true,
			["coverage"] = "name-query",
			["expectedItems"] = {
				36932, -- [1]
				39968, -- [2]
			},
			["observedItems"] = {
				36932, -- [1]
			},
			["lots"] = {
				"36932:0:2:1898000:2000000:0:3:Дневнойсвет", -- [1]
				"36932:0:1:990000:1000000:0:2:Nadzh", -- [2]
			},
		}, -- [5]
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792903000|6",
			["t"] = 1792903000,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "pages",
			["total"] = 1,
			["secs"] = 9,
			["expect"] = 1,
			["miss"] = 0,
			["scope"] = "gems",
			["gemGroup"] = "epic",
			["query"] = "",
			["classIndex"] = 8,
			["minQuality"] = 4,
			["lots"] = {
				"36919:0:1:950000:1000000:0:4:Вивалякуба", -- [1]
			},
		}, -- [6]
		{
			["id"] = "WoW Circle 3.3.5a x1|Alliance|1792903600|7",
			["t"] = 1792903600,
			["realm"] = "WoW Circle 3.3.5a x1",
			["faction"] = "Alliance",
			["mode"] = "pages",
			["total"] = 1,
			["secs"] = 13,
			["expect"] = 1152,
			["miss"] = 0,
			["scope"] = "disenchant",
			["deGroup"] = "wrathArmorGreen",
			["query"] = "",
			["classIndex"] = 2,
			["minLevel"] = 68,
			["maxLevel"] = 80,
			["minQuality"] = 2,
			["truncated"] = true,
			["deCut"] = 1,
			["deReadPages"] = 3,
			["deTarget"] = 34054,
			["disenchantItems"] = {
				[36230] = {
					["band"] = "wrathArmorHigh",
					["required"] = 73,
					["level"] = 154,
					["skill"] = 350,
					["classIndex"] = 2,
				},
			},
			["lots"] = {
				"36230:0:1:30000:35000:0:2:Страхнул", -- [1]
			},
		}, -- [7]
	},
	["items"] = {
		[36919] = "Багровый рубин	3",
		[34054] = "Абсолютная пыль	1",
		[43102] = "Замёрзшая сфера	2",
		[36932] = "Темный нефрит	2",
	},
	["sharedHistory"] = {
		["circle|WoW Circle 3.3.5a x1|Alliance|faction"] = {
			["count"] = 1,
			["items"] = {
				[36919] = {
					{
						["sourceID"] = "other",
						["scanID"] = "remote-1",
						["at"] = 1792896400,
						["since"] = 1792896340,
						["price"] = 990000,
						["p25"] = 980000,
						["lots"] = 3,
						["units"] = 3,
						["clockStatus"] = "unverified",
					}, -- [1]
				},
			},
		},
	},
	["ledger"] = {
		["v"] = 1,
		["events"] = {
		},
	},
	["log"] = {
		"26.10 12:00:00 done pages lots=2 expect=2 miss=0 secs=2", -- [1]
	},
	["priceSchema"] = 2,
	["auctionFee"] = 0.05,
}
