-- Demo presets and creatives. Images are served by proview-web under /banners/.
INSERT INTO placements (id, description, width, height) VALUES
    ('home_top', 'Leaderboard at the top of the landing page', 728, 90),
    ('sidebar',  'Medium rectangle in the right column',      300, 250),
    ('footer',   'Billboard above the footer',                 970, 90)
ON CONFLICT (id) DO NOTHING;

INSERT INTO banners (name, image_url, click_url, alt, width, height, weight, placements, countries, devices)
SELECT * FROM (VALUES
    ('Aurora Cloud — leaderboard', '/banners/aurora-728x90.svg',  'https://example.com/aurora?utm_source=proview', 'Aurora Cloud', 728, 90,  300, ARRAY['home_top'], ARRAY[]::text[], ARRAY[]::text[]),
    ('Nimbus Bank — leaderboard',  '/banners/nimbus-728x90.svg',  'https://example.com/nimbus?utm_source=proview', 'Nimbus Bank',  728, 90,  100, ARRAY['home_top'], ARRAY[]::text[], ARRAY[]::text[]),
    ('Aurora Cloud — rectangle',   '/banners/aurora-300x250.svg', 'https://example.com/aurora?utm_source=proview', 'Aurora Cloud', 300, 250, 200, ARRAY['sidebar'],  ARRAY[]::text[], ARRAY[]::text[]),
    ('Orbit Travel — rectangle',   '/banners/orbit-300x250.svg',  'https://example.com/orbit?utm_source=proview',  'Orbit Travel', 300, 250, 200, ARRAY['sidebar'],  ARRAY[]::text[], ARRAY[]::text[]),
    ('Orbit Travel — mobile only', '/banners/orbit-300x250.svg',  'https://example.com/orbit-app?utm_source=proview', 'Orbit Travel app', 300, 250, 300, ARRAY['sidebar'], ARRAY[]::text[], ARRAY['mobile','tablet']),
    ('Nimbus Bank — billboard',    '/banners/nimbus-970x90.svg',  'https://example.com/nimbus?utm_source=proview', 'Nimbus Bank',  970, 90,  100, ARRAY['footer'],   ARRAY[]::text[], ARRAY[]::text[])
) AS v(name, image_url, click_url, alt, width, height, weight, placements, countries, devices)
WHERE NOT EXISTS (SELECT 1 FROM banners);
