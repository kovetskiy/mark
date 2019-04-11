tests:ensure :run --version

blank:server:new wiki

tests:ensure $wiki::start

tests:eval $wiki::get-listen
listen=$(tests:get-stdout)

tests:debug "listening on ${listen}"

$wiki::stop
