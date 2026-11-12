import json, sys

TABS = [
    {"icon": "house", "title": "Today"},
    {"icon": "calendar", "title": "Programme"},
    {"icon": "map", "title": "Venue"},
    {"icon": "bookmark", "title": "Agenda"},
    {"icon": "id-card", "title": "Badge"},
]

ops = [
    {"op": "set_theme", "name": "ios", "accent": "teal"},
    {"op": "update_page", "name": "COP31 Companion", "background": "#E9ECEF"},
]

X = 0


def screen(n, name, **props):
    global X
    sid = f"s{n:02d}"
    p = dict(props)
    p.setdefault("tabs", TABS)
    ops.append({"op": "insert", "block": "ios/shell", "id": sid, "x": X, "y": 0, "props": p})
    ops.append({"op": "update", "id": sid, "props": {"name": f"{n:02d} {name}"}})
    X += 470
    return sid + "-body"


def put(body, block, **props):
    ops.append({"op": "insert", "block": block, "parent": body, "props": props})


# 01 Onboarding ---------------------------------------------------------
b = screen(1, "Onboarding", nav="off", tabbar="off", status="accent")
put(b, "ios/hero", eyebrow="UN Climate Change · Antalya", title="COP31 Companion",
    text="Programme, venue and negotiations for the conference at Antalya EXPO Center.",
    chips=[{"title": "9–20 Nov 2026"}, {"title": "Türkiye"}])
put(b, "ios/section", title="Language")
put(b, "ios/segmented", segments=[{"title": "English"}, {"title": "Türkçe"}], selected="0")
put(b, "ios/section", title="Your role")
put(b, "ios/list", rows=[
    {"icon": "landmark", "title": "Party delegate", "text": "Negotiations in the Blue Zone", "accessory": "check", "chevron": "off"},
    {"icon": "eye", "title": "Observer", "text": "Admitted organisations"},
    {"icon": "mic", "title": "Media", "text": "Press centre and briefings"},
    {"icon": "users", "title": "Visitor", "text": "Green Zone, open to the public"},
], footer="Blue Zone access needs UNFCCC accreditation.")
put(b, "ios/spacer")
put(b, "ios/button", label="Enter the conference", style="filled")

# 02 Home ---------------------------------------------------------------
b = screen(2, "Home", title="Today", action="bell", active="0")
put(b, "ios/highlight", label="Day", value="2", title="Tuesday 10 November",
    text="Conference day 2 of 12", progress="0.17", meta="Closes Friday 20 November")
put(b, "ios/section", title="Now and next", action="Programme")
put(b, "ios/schedule", items=[
    {"time": "10:30", "end": "11:45", "title": "Climate-resilient cities: financing local action", "meta": "Sustainable Cities hub", "zone": "green", "now": "true"},
    {"time": "15:00", "end": "16:30", "title": "Plenary: stocktake of negotiations", "meta": "Plenary hall", "zone": "blue"},
])
put(b, "ios/tiles", items=[
    {"icon": "calendar", "title": "Programme"}, {"icon": "map", "title": "Venue"},
    {"icon": "bus", "title": "Shuttles"}, {"icon": "layers", "title": "Themes"}])
put(b, "ios/banner", tone="info", icon="shield-check",
    text="Leaders' Summit, 11–12 November: expect extra security checks.")

# 03 Programme ----------------------------------------------------------
b = screen(3, "Programme", title="Programme", action="search", active="1")
days = [
    {"d": "9", "l": "Mon"}, {"d": "10", "l": "Tue"}, {"d": "11", "l": "Summit", "kind": "event"},
    {"d": "12", "l": "Summit", "kind": "event"}, {"d": "13", "l": "Fri"}, {"d": "14", "l": "Youth", "kind": "theme"},
    {"d": "15", "l": "Culture", "kind": "theme"}, {"d": "16", "l": "Mon"}, {"d": "17", "l": "Tue"},
    {"d": "18", "l": "Wed"}, {"d": "19", "l": "İmece", "kind": "theme"}, {"d": "20", "l": "Close", "kind": "theme"},
]
put(b, "ios/day-strip", days=days, selected="1", per_row="6")
put(b, "ios/chips", chips=[{"title": "All"}, {"title": "Blue Zone"}, {"title": "Green Zone"}], selected="0")
put(b, "ios/schedule", items=[
    {"time": "09:00", "end": "10:15", "title": "Energy and Transport day opens", "meta": "Thematic hub", "zone": "green"},
    {"time": "10:30", "end": "11:45", "title": "Climate-resilient cities: financing local action", "meta": "Sustainable Cities hub", "zone": "green", "now": "true"},
    {"time": "13:00", "end": "14:00", "title": "Side event: methane and waste", "meta": "Side event room", "zone": "blue"},
    {"time": "15:00", "end": "16:30", "title": "Plenary: stocktake of negotiations", "meta": "Plenary hall", "zone": "blue"},
])

# 04 Session detail -----------------------------------------------------
b = screen(4, "Session detail", title="Session", back="Programme", action="share-2", style="inline", tabbar="off")
put(b, "ios/detail-head", tag="Green Zone · Sustainable Cities", tone="green",
    title="Climate-resilient cities: financing local action", facts=[
        {"icon": "calendar", "text": "Tuesday 10 November, 10:30–11:45"},
        {"icon": "map-pin", "text": "Sustainable Cities hub, Green Zone"},
        {"icon": "users", "text": "Open to registered visitors"}])
put(b, "ios/text", title="About this session",
    text="How cities pay for heat, flood and transport resilience, and what national and international finance can do to reach them. Part of the Climate-Resilient Cities theme of the Action Agenda.")
put(b, "ios/banner", tone="info", icon="info", text="Speakers are announced by the organisers closer to the session.")
put(b, "ios/spacer")
put(b, "ios/action-bar", cta="Add to my agenda", icons="share-2|map-pin")

# 05 Venue map ----------------------------------------------------------
b = screen(5, "Venue map", title="Venue", style="inline", active="2")
put(b, "ios/segmented", segments=[{"title": "Plan"}, {"title": "Services"}], selected="0")
put(b, "ios/map", height="304", zones=[
    {"name": "Blue Zone", "kind": "blue", "x": "14", "y": "14", "w": "156", "h": "170", "items": "Plenary halls|Delegation offices|Media centre|Pavilions"},
    {"name": "Green Zone", "kind": "green", "x": "188", "y": "14", "w": "156", "h": "170", "items": "Zero Waste|Climate Finance|Energy and Transport|Startups|COP-Art"},
    {"name": "Entrance plaza", "kind": "neutral", "x": "14", "y": "200", "w": "330", "h": "76", "items": "Security check|Transfer point"},
], pins=[{"x": "236", "y": "238", "label": "You", "kind": "you"}])
put(b, "ios/list", header="Getting in", rows=[
    {"icon": "id-card", "title": "Blue Zone entrance", "text": "Accredited badge and photo ID"},
    {"icon": "users", "title": "Green Zone entrance", "text": "Registration required, open to the public"},
    {"icon": "accessibility", "title": "Accessible route", "text": "Step-free from the plaza to the plenary"},
])

# 06 Pavilions ----------------------------------------------------------
b = screen(6, "Pavilions", title="Pavilions", back="Venue", style="inline", active="2")
put(b, "ios/search", placeholder="Search countries and organisations")
put(b, "ios/chips", chips=[{"title": "All"}, {"title": "Countries"}, {"title": "Organisations"}], selected="0")
put(b, "ios/list", rows=[
    {"icon": "flag", "title": "Türkiye Pavilion", "text": "Host country"},
    {"icon": "globe", "title": "Mediterranean Pavilion", "text": "Union for the Mediterranean"},
    {"icon": "leaf", "title": "Carbon Removals Pavilion", "text": "Dedicated UNFCCC pavilion"},
    {"icon": "factory", "title": "UNIDO Pavilion", "text": "Industry and green industrialisation"},
    {"icon": "handshake", "title": "IETA Business Pavilion", "text": "Carbon markets and business"},
    {"icon": "users", "title": "IUCN Pavilion", "text": "Nature and WWF Panda Hub"},
])

# 07 Action Agenda ------------------------------------------------------
b = screen(7, "Action Agenda", title="Action Agenda", back="Today", style="inline", active="0")
put(b, "ios/text", title="Ten priority themes",
    text="The presidency's Action Agenda groups work into ten themes. Tap a theme to see its events and pavilions.")
put(b, "ios/grid", items=[
    {"icon": "recycle", "title": "Zero Waste and Methane", "text": "Cut waste and methane."},
    {"icon": "waves", "title": "Oceans and Seas", "text": "Protect coasts and marine life."} if False else {"icon": "globe", "title": "Oceans and Seas", "text": "Protect coasts and marine life."},
    {"icon": "wheat", "title": "Food Security", "text": "Sustainable agriculture."},
    {"icon": "building", "title": "Resilient Cities", "text": "Heat, floods and transport."},
    {"icon": "heart-pulse", "title": "Health Systems", "text": "Dynamic and resilient care."},
    {"icon": "graduation-cap", "title": "Youth and Education", "text": "Skills for a green future."},
])

# 08 Negotiations -------------------------------------------------------
b = screen(8, "Negotiations", title="Negotiations", active="1")
put(b, "ios/people", items=[
    {"name": "Murat Kurum", "role": "COP31 President · Türkiye", "initials": "MK"},
    {"name": "Chris Bowen", "role": "President of Negotiations · Australia", "initials": "CB"},
])
put(b, "ios/section", title="Agenda items", action="UNFCCC portal")
put(b, "ios/status-list", items=[
    {"title": "Climate finance", "text": "Draft text under consultation", "status": "Draft", "tone": "info"},
    {"title": "Adaptation", "text": "Contact group meets Wednesday", "status": "Active", "tone": "success"},
    {"title": "Mitigation work programme", "text": "Informal consultations", "status": "Informal", "tone": "warning"},
])
put(b, "ios/banner", tone="info", icon="info", text="Statuses are illustrative. Official texts are published on the UNFCCC website.")

# 09 Getting around -----------------------------------------------------
b = screen(9, "Getting around", title="Getting around", back="Venue", style="inline", active="2")
put(b, "ios/list", header="To the venue", rows=[
    {"icon": "bus", "title": "Conference shuttles", "text": "Hotels to Antalya EXPO Center"},
    {"icon": "plane", "title": "Antalya Airport", "text": "Transfers to the city and venue"},
    {"icon": "car", "title": "Taxi and drop-off", "text": "Drop-off at the entrance plaza"},
])
put(b, "ios/section", title="Live status")
put(b, "ios/status-list", items=[
    {"title": "Shuttles", "text": "Running every 15 minutes", "status": "Normal", "tone": "success"},
    {"title": "Entrance queues", "text": "Security check at the plaza", "status": "Busy", "tone": "warning"},
])
put(b, "ios/banner", tone="info", icon="info", text="Live status is a demonstration. Check the Presidency's transport notices before you travel.")

# 10 My agenda ----------------------------------------------------------
b = screen(10, "My agenda", title="My agenda", active="3")
put(b, "ios/segmented", segments=[{"title": "Saved"}, {"title": "Reminders"}], selected="0")
put(b, "ios/banner", tone="warning", icon="clock", text="Two saved sessions overlap on Wednesday 11 November at 14:00.")
put(b, "ios/schedule", items=[
    {"time": "10:30", "end": "11:45", "title": "Climate-resilient cities: financing local action", "meta": "Tue 10 Nov", "zone": "green"},
    {"time": "14:00", "end": "15:00", "title": "Youth and education: skills for climate action", "meta": "Wed 11 Nov", "zone": "green"},
    {"time": "14:00", "end": "15:30", "title": "Side event: carbon removals", "meta": "Wed 11 Nov", "zone": "blue"},
])
put(b, "ios/list", header="Reminders", rows=[
    {"icon": "bell", "title": "Remind me 30 minutes before", "accessory": "toggle"},
    {"icon": "calendar", "title": "Add to Calendar", "text": "Export saved sessions"},
])

# 11 Notifications ------------------------------------------------------
b = screen(11, "Notifications", title="Notifications", back="Today", style="inline", tabbar="off")
put(b, "ios/notifications", items=[
    {"icon": "map-pin", "title": "Room change", "text": "Side event moved to the side event room, Blue Zone.", "time": "5 min", "unread": "true", "tone": "warning"},
    {"icon": "bus", "title": "Shuttle update", "text": "Evening shuttles run until 22:00.", "time": "1 h", "unread": "true", "tone": "info"},
    {"icon": "gavel", "title": "Plenary at 15:00", "text": "Stocktake of negotiations in the plenary hall.", "time": "2 h", "tone": "accent"},
    {"icon": "calendar", "title": "Agenda reminder", "text": "Your saved session starts in 30 minutes.", "time": "Yesterday", "tone": "accent"},
    {"icon": "shield-check", "title": "Leaders' Summit", "text": "Extra security checks on 11 and 12 November.", "time": "Yesterday", "tone": "warning"},
    {"icon": "layers", "title": "New session added", "text": "Zero Waste hub: methane in the food chain, Thursday.", "time": "Mon", "tone": "accent"},
    {"icon": "map-pin", "title": "Green Zone opens at 09:00", "text": "Registered visitors can enter through the plaza.", "time": "Mon", "tone": "accent"},
])

# 12 Badge --------------------------------------------------------------
b = screen(12, "Badge", title="Badge", style="inline", action="settings", active="4")
put(b, "ios/badge", event="COP31 Antalya", name="Alex Morgan", role="Observer · Climate Network",
    code="COP31-OBS-4821", valid="Valid 9–20 November 2026 with photo ID",
    access=[{"title": "Blue Zone", "on": "true"}, {"title": "Green Zone", "on": "true"}])
put(b, "ios/list", rows=[
    {"icon": "download", "title": "Save to Wallet", "text": "Works offline"},
    {"icon": "languages", "title": "Language", "value": "English"},
])

# 13 Settings -----------------------------------------------------------
b = screen(13, "Settings", title="Settings", back="Badge", style="inline", tabbar="off")
put(b, "ios/list", header="General", rows=[
    {"icon": "languages", "title": "Language", "value": "English"},
    {"icon": "accessibility", "title": "Accessibility"},
    {"icon": "wifi-off", "title": "Offline maps", "accessory": "toggle"},
])
put(b, "ios/list", header="Notifications", rows=[
    {"icon": "bell", "title": "Room changes", "accessory": "toggle"},
    {"icon": "gavel", "title": "Plenary calls", "accessory": "toggle"},
    {"icon": "calendar", "title": "Agenda reminders", "accessory": "toggle", "value": "off"},
])
put(b, "ios/list", header="About", rows=[
    {"icon": "info", "title": "Version", "value": "1.0", "chevron": "off"},
    {"icon": "file-text", "title": "Sources and sample content"},
], footer="Programme times and statuses are illustrative.")

json.dump(ops, open(sys.argv[1], "w", encoding="utf-8"), ensure_ascii=False, indent=1)
print(len(ops), "ops")
