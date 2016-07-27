// Autogenerated by gen_tests.py and process_polyglot.py.
// Do not edit this file directly.
// The template for this file is located at:
// ../../gen_tests/template.go.tpl
package reql_tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	r "gopkg.in/dancannon/gorethink.v2"
)

// Tests timeouts.
func TestTimeoutSuite(t *testing.T) {
    suite.Run(t, new(TimeoutSuite ))
}

type TimeoutSuite struct {
	suite.Suite

	session *r.Session
}

func (suite *TimeoutSuite) SetupTest() {
	suite.T().Log("Setting up TimeoutSuite")
	// Use imports to prevent errors
	time.Now()

	session, err := r.Connect(r.ConnectOpts{
		Address: url,
	})
	suite.Require().NoError(err, "Error returned when connecting to server")
	suite.session = session

    r.DBDrop("test").Exec(suite.session)
	err = r.DBCreate("test").Exec(suite.session)
	suite.Require().NoError(err)
	err = r.DB("test").Wait().Exec(suite.session)
	suite.Require().NoError(err)

}

func (suite *TimeoutSuite) TearDownSuite() {
	suite.T().Log("Tearing down TimeoutSuite")

	r.DB("rethinkdb").Table("_debug_scratch").Delete().Exec(suite.session)
    r.DBDrop("test").Exec(suite.session)

    suite.session.Close()
}

func (suite *TimeoutSuite) TestCases() {
	suite.T().Log("Running TimeoutSuite: Tests timeouts.")



    {
        // timeout.yaml line #32
        /* err("ReqlNonExistenceError", "Error in HTTP GET of `httpbin.org/delay/10`:" + " timed out after 0.800 seconds.", []) */
        var expected_ Err = err("ReqlNonExistenceError", "Error in HTTP GET of `httpbin.org/delay/10`:" + " timed out after 0.800 seconds.")
        /* r.http('httpbin.org/delay/10', timeout=0.8) */

    	suite.T().Log("About to run line #32: r.HTTP('httpbin.org/delay/10', r.HTTPOpts{Timeout: 0.8, })")

        runAndAssert(suite.Suite, expected_, r.HTTP("httpbin.org/delay/10", r.HTTPOpts{Timeout: 0.8, }), suite.session, r.RunOpts{
			GeometryFormat: "raw",
    	})
        suite.T().Log("Finished running line #32")
    }

    {
        // timeout.yaml line #36
        /* err("ReqlNonExistenceError", "Error in HTTP PUT of `httpbin.org/delay/10`:" + " timed out after 0.000 seconds.", []) */
        var expected_ Err = err("ReqlNonExistenceError", "Error in HTTP PUT of `httpbin.org/delay/10`:" + " timed out after 0.000 seconds.")
        /* r.http('httpbin.org/delay/10', method='PUT', timeout=0.0) */

    	suite.T().Log("About to run line #36: r.HTTP('httpbin.org/delay/10', r.HTTPOpts{Method: 'PUT', Timeout: 0.0, })")

        runAndAssert(suite.Suite, expected_, r.HTTP("httpbin.org/delay/10", r.HTTPOpts{Method: "PUT", Timeout: 0.0, }), suite.session, r.RunOpts{
			GeometryFormat: "raw",
    	})
        suite.T().Log("Finished running line #36")
    }
}