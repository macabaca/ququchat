const BASE_URL: string = "http://localhost:8080/api"

const WHITE_LIST: Array<string> = ['/auth', '/auth/login', '/auth/register', '/auth/refresh']
const REQUEST_URI: Map<string,string> = new Map<string,string>([
    ["login", "/auth/login"],
    ["register", "/auth/register"],
    ["refresh", "/auth/refresh"],
    ["logout", "/auth/logout"],
])

export {
    BASE_URL,
    WHITE_LIST,
    REQUEST_URI
}